// Package usage turns the transcripts' token counts into the Quota tab's data.
//
// A transcript is read once: the scanner remembers, per file, how far it read,
// and keeps only per-bucket sums (10 minutes, per model family), cached under the
// user cache directory, so a restart reads what was appended and not gigabytes.
// Only numbers are decoded: the struct has no field for message content.
package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-agentsync/agentscope/accounts"
	"github.com/go-agentsync/agentscope/model"
	"github.com/go-agentsync/agentscope/quota"
)

// Families are the stack order of the charts.
var Families = model.Families

// BucketSec is the resolution of the kept sums.
const BucketSec = 600

// Keep is how far back sums are kept: four 7 day windows and a week of margin
// for the alignment on the reset.
const Keep = 35 * 24 * time.Hour

// KeepRefusals is how far back gate refusals are kept: the Guardrails tab's 12
// weeks and a week of margin for the alignment on Monday.
const KeepRefusals = 13 * 7 * 24 * time.Hour

// Bounds on what is read.
const (
	maxLine   = 4 << 20 // a longer line is skipped, not parsed
	recentIDs = 16      // message ids remembered per file for the dedup
)

// Counts is the tokens of one family in one bucket.
type Counts struct {
	Out       int64 `json:"o,omitempty"`
	CacheRead int64 `json:"c,omitempty"`
}

// recent is a message id seen in a file and what was counted for it, so a later
// entry of the same API message replaces its counts instead of adding to them.
type recent struct {
	ID     string `json:"i"`
	Acct   string `json:"a,omitempty"`
	Bucket int64  `json:"b"`
	Family int    `json:"f"`
	Out    int64  `json:"o"`
	Cache  int64  `json:"c"`
}

type fileState struct {
	// Owner is the account of the latest session-owner entry read in this file:
	// the account every later assistant entry is attributed to. "" until the
	// file has shown one: those entries are unattributed, never guessed.
	Owner  string   `json:"own,omitempty"`
	Offset int64    `json:"off"`
	Size   int64    `json:"size"`
	Recent []recent `json:"recent,omitempty"`
}

// Stats says what a Refresh did, for tests and for the cold-scan measurement.
type Stats struct {
	Files, Scanned int
	Bytes          int64
	Lines, Counted int
	Skipped        int // lines over maxLine
	Bad            int // usage lines that did not decode
}

// sumKey is one bucket's tokens of one family for one account ("" is
// unattributed).
type sumKey struct {
	Bucket int64
	Acct   string
	Fam    int
}

// Scanner holds the per-file offsets and the per-bucket sums.
type Scanner struct {
	root  string // ~/.claude/projects
	cache string // cache file, "" for none
	files map[string]*fileState
	sums  map[sumKey]Counts
	// refs are the gate refusals, per hour and gate (see refusal.go).
	refs  map[refKey]int64
	dirty bool
	// stamps is the `stamp` hook's timeline: the fallback for a call whose
	// transcript has shown no session-owner entry.
	stamps accounts.Timeline
	// refCut is the oldest instant a refusal is kept from, set by Refresh;
	// scanned is whether a Refresh ended (before it, refusals are "not measured").
	refCut  time.Time
	scanned bool
}

// NewScanner reads transcripts under root and keeps its cache in cacheFile
// ("" keeps none). A cache that cannot be read is ignored: it is rebuilt.
func NewScanner(root, cacheFile string) *Scanner {
	s := &Scanner{root: root, cache: cacheFile, files: map[string]*fileState{}, sums: map[sumKey]Counts{}, refs: map[refKey]int64{}}
	s.load()
	return s
}

// SetStamps gives the scanner the timeline of the `stamp` hook, used for the
// calls of a transcript that has no session-owner entry.
func (s *Scanner) SetStamps(tl accounts.Timeline) { s.stamps = tl }

// Attribute is the account of a call made at `at` in a transcript whose latest
// session-owner entry named owner: that one when there is one, else the latest
// stamp at or before the call, else "" (unattributed).
func (s *Scanner) Attribute(owner string, at time.Time) string {
	if owner != "" {
		return owner
	}
	if st, ok := s.stamps.At(at); ok {
		return st.AccountHash
	}
	return ""
}

// usageLine is the metadata of an assistant entry that is read. There is no
// field for message content.
type usageLine struct {
	Timestamp string `json:"timestamp"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage struct {
			Output    int64 `json:"output_tokens"`
			CacheRead int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

var (
	assistantMark = []byte(`"type":"assistant"`)
	usageMark     = []byte(`"usage"`)
	ownerMark     = []byte(`"type":"session-owner"`)
)

// ownerLine is a session-owner entry: Claude Code writes one when a session
// starts or resumes, naming the account it runs as. It has no timestamp: its
// position in the file is what orders it among the assistant entries.
type ownerLine struct {
	AccountUUID string `json:"accountUuid"`
}

func familyIndex(modelID string) int {
	f := model.ModelFamily(modelID)
	for i, n := range Families {
		if n == f {
			return i
		}
	}
	return -1
}

// Refresh scans what changed since the last pass and drops sums older than
// Keep. It returns what it did.
func (s *Scanner) Refresh(now time.Time) Stats {
	var st Stats
	cutoff := now.Add(-Keep)
	// Refusals reach further back than tokens, so a file is read from as far back
	// as they do (its token lines older than cutoff are skipped by their dates).
	s.refCut = now.Add(-KeepRefusals)
	seen := map[string]bool{}
	_ = filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(s.refCut) {
			return nil
		}
		st.Files++
		idle := now.Sub(info.ModTime()) > 2*time.Hour
		seen[p] = true
		fs := s.files[p]
		if fs == nil {
			fs = &fileState{}
			s.files[p] = fs
		}
		if info.Size() == fs.Size && fs.Offset == info.Size() {
			if idle {
				fs.Recent = nil // a message is never split across a two hour gap
			}
			return nil
		}
		s.scanFile(p, info.Size(), fs, cutoff, &st)
		return nil
	})
	for p := range s.files {
		if !seen[p] {
			delete(s.files, p)
			s.dirty = true
		}
	}
	for k := range s.sums {
		if k.Bucket < cutoff.Unix()-BucketSec {
			delete(s.sums, k)
			s.dirty = true
		}
	}
	for k := range s.refs {
		if k.Hour < s.refCut.Unix()-refHour {
			delete(s.refs, k)
			s.dirty = true
		}
	}
	s.scanned = true
	s.save()
	return st
}

func (s *Scanner) scanFile(p string, size int64, fs *fileState, cutoff time.Time, st *Stats) {
	f, err := os.Open(p)
	if err != nil {
		return
	}
	defer f.Close()
	if size < fs.Offset { // rewritten: start over (its old sums stay; ids restart)
		*fs = fileState{}
	}
	if _, err := f.Seek(fs.Offset, io.SeekStart); err != nil {
		return
	}
	st.Scanned++
	br := bufio.NewReaderSize(f, 64<<10)
	pos := fs.Offset
	for {
		line, n, complete := readLine(br)
		if !complete {
			break
		}
		pos += n
		st.Bytes += n
		st.Lines++
		if line == nil {
			st.Skipped++
			continue
		}
		s.fold(line, fs, cutoff, st)
	}
	fs.Offset, fs.Size = pos, size
	s.dirty = true
}

// fold adds one transcript line to the sums.
func (s *Scanner) fold(line []byte, fs *fileState, cutoff time.Time, st *Stats) {
	if bytes.Contains(line, ownerMark) {
		var o ownerLine
		if json.Unmarshal(line, &o) == nil && o.AccountUUID != "" {
			fs.Owner = quota.AccountKey(o.AccountUUID)
		}
		return
	}
	if s.foldRefusals(line) {
		return
	}
	if !bytes.Contains(line, assistantMark) || !bytes.Contains(line, usageMark) {
		return
	}
	var u usageLine
	if err := json.Unmarshal(line, &u); err != nil {
		var te *json.UnmarshalTypeError
		if !errors.As(err, &te) {
			st.Bad++
			return
		}
	}
	fam := familyIndex(u.Message.Model)
	at, err := time.Parse(time.RFC3339Nano, u.Timestamp)
	if fam < 0 || err != nil || at.Before(cutoff) {
		return
	}
	// One message cannot hold more than a trillion tokens; the bound keeps a
	// hostile line from overflowing a sum.
	const maxTokens = 1 << 40
	out := min(max(u.Message.Usage.Output, 0), maxTokens)
	cr := min(max(u.Message.Usage.CacheRead, 0), maxTokens)
	bucket := at.Unix() - at.Unix()%BucketSec
	// Whose call it was: the session-owner entry of this transcript when it has
	// shown one, else the account the stamps say was signed in at that moment,
	// else nobody's: unattributed, never guessed.
	acct := s.Attribute(fs.Owner, at)
	// One API message is split over several entries carrying the same id: its
	// counts are replaced by the larger ones, never added twice.
	if id := u.Message.ID; id != "" {
		for i := range fs.Recent {
			r := &fs.Recent[i]
			if r.ID != id {
				continue
			}
			s.add(r.Bucket, r.Acct, r.Family, max(out, r.Out)-r.Out, max(cr, r.Cache)-r.Cache)
			r.Out, r.Cache = max(out, r.Out), max(cr, r.Cache)
			return
		}
		fs.Recent = append(fs.Recent, recent{ID: id, Acct: acct, Bucket: bucket, Family: fam, Out: out, Cache: cr})
		if len(fs.Recent) > recentIDs {
			fs.Recent = fs.Recent[len(fs.Recent)-recentIDs:]
		}
	}
	s.add(bucket, acct, fam, out, cr)
	st.Counted++
}

func (s *Scanner) add(bucket int64, acct string, fam int, out, cr int64) {
	if out == 0 && cr == 0 {
		return
	}
	k := sumKey{bucket, acct, fam}
	c := s.sums[k]
	c.Out += out
	c.CacheRead += cr
	s.sums[k] = c
	s.dirty = true
}

// readLine returns the next newline-terminated line; a line over maxLine is
// consumed and returned nil. complete is false at the end of input, or when the
// last line has no newline yet (it is left for the next pass).
func readLine(br *bufio.Reader) (line []byte, n int64, complete bool) {
	var buf []byte
	over := false
	for {
		b, err := br.ReadSlice('\n')
		n += int64(len(b))
		if !over {
			if len(buf)+len(b) > maxLine {
				over, buf = true, nil
			} else {
				buf = append(buf, b...)
			}
		}
		switch {
		case err == nil:
			// buf is nil exactly when the line was over the bound (a line holds at
			// least its newline, so a kept one is never nil): the caller counts a
			// nil line as skipped.
			return buf, n, true
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		default:
			return nil, 0, false
		}
	}
}

// Accounts is the keys of the accounts that have tokens, and whether any
// tokens are unattributed.
func (s *Scanner) Accounts() (keys []string, unattributed bool) {
	seen := map[string]bool{}
	for k := range s.sums {
		if k.Acct == "" {
			unattributed = true
		} else if !seen[k.Acct] {
			seen[k.Acct] = true
			keys = append(keys, k.Acct)
		}
	}
	sort.Strings(keys)
	return keys, unattributed
}

// Which says whose tokens a series counts.
type Which int

const (
	// Only counts one account (a key).
	Only Which = iota
	// Every counts every account and the unattributed tokens, the latter as an
	// extra last layer.
	Every
)

// Buckets returns n buckets of step seconds from start: out[layer][i] and
// cacheRead[layer][i], one layer per family, and for Every one more for the
// unattributed tokens. step is a multiple of BucketSec.
func (s *Scanner) Buckets(start, step int64, n int, w Which, acct string) (out, cacheRead [][]float64) {
	layers := len(Families)
	if w == Every {
		layers++
	}
	out = make([][]float64, layers)
	cacheRead = make([][]float64, layers)
	for f := range out {
		out[f], cacheRead[f] = make([]float64, n), make([]float64, n)
	}
	for k, v := range s.sums {
		i := int((k.Bucket - start) / step)
		if k.Bucket < start || i >= n {
			continue
		}
		layer := k.Fam
		switch {
		case w == Only && k.Acct != acct:
			continue
		case w == Every && k.Acct == "":
			layer = len(Families)
		}
		out[layer][i] += float64(v.Out)
		cacheRead[layer][i] += float64(v.CacheRead)
	}
	return out, cacheRead
}

// cacheFile is what is kept on disk.
type cacheFile struct {
	Version int                   `json:"v"`
	Files   map[string]*fileState `json:"files"`
	Sums    []cachedSum           `json:"sums"`
	Refs    []cachedRef           `json:"refs,omitempty"`
}

// cachedSum is a sum as kept on disk (a JSON object cannot be keyed by a struct).
type cachedSum struct {
	B int64  `json:"b"`
	A string `json:"a,omitempty"`
	F int    `json:"f"`
	C Counts `json:"c"`
}

// The version 1 cache had no accounts, the version 2 no refusals; each is rebuilt.
const cacheVersion = 3

func (s *Scanner) load() {
	// No cache path reads nothing: ReadFile("") is an error, so an empty path
	// needs no refusal of its own.
	b, err := os.ReadFile(s.cache)
	if err != nil {
		return
	}
	var c cacheFile
	if json.Unmarshal(b, &c) != nil || c.Version != cacheVersion {
		return
	}
	for p, f := range c.Files {
		if f != nil {
			s.files[p] = f
		}
	}
	for _, c := range c.Sums {
		if c.F >= 0 && c.F < len(Families) {
			s.sums[sumKey{c.B, c.A, c.F}] = c.C
		}
	}
	for _, r := range c.Refs {
		if r.N > 0 && r.G != "" {
			s.refs[refKey{r.H, r.G}] = r.N
		}
	}
}

func (s *Scanner) save() {
	if s.cache == "" || !s.dirty {
		return
	}
	sums := make([]cachedSum, 0, len(s.sums))
	for k, v := range s.sums {
		sums = append(sums, cachedSum{k.Bucket, k.Acct, k.Fam, v})
	}
	b, err := json.Marshal(cacheFile{Version: cacheVersion, Files: s.files, Sums: sums, Refs: s.cachedRefs()})
	if err != nil || os.MkdirAll(filepath.Dir(s.cache), 0o700) != nil {
		return
	}
	tmp := s.cache + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil && os.Rename(tmp, s.cache) == nil {
		s.dirty = false
	}
}
