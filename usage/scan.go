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
	"strings"
	"time"

	"github.com/go-agentsync/agentscope/model"
)

// Families are the stack order of the charts.
var Families = model.Families

// BucketSec is the resolution of the kept sums.
const BucketSec = 600

// Keep is how far back sums are kept: four 7 day windows and a week of margin
// for the alignment on the reset.
const Keep = 35 * 24 * time.Hour

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
	Bucket int64  `json:"b"`
	Family int    `json:"f"`
	Out    int64  `json:"o"`
	Cache  int64  `json:"c"`
}

type fileState struct {
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

// Scanner holds the per-file offsets and the per-bucket sums.
type Scanner struct {
	root  string // ~/.claude/projects
	cache string // cache file, "" for none
	files map[string]*fileState
	sums  map[int64][]Counts // bucket start -> per family
	dirty bool
}

// NewScanner reads transcripts under root and keeps its cache in cacheFile
// ("" keeps none). A cache that cannot be read is ignored: it is rebuilt.
func NewScanner(root, cacheFile string) *Scanner {
	s := &Scanner{root: root, cache: cacheFile, files: map[string]*fileState{}, sums: map[int64][]Counts{}}
	s.load()
	return s
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
)

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
	seen := map[string]bool{}
	_ = filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(cutoff) {
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
	for b := range s.sums {
		if b < cutoff.Unix()-BucketSec {
			delete(s.sums, b)
			s.dirty = true
		}
	}
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
	// One API message is split over several entries carrying the same id: its
	// counts are replaced by the larger ones, never added twice.
	if id := u.Message.ID; id != "" {
		for i := range fs.Recent {
			r := &fs.Recent[i]
			if r.ID != id {
				continue
			}
			s.add(r.Bucket, r.Family, max(out, r.Out)-r.Out, max(cr, r.Cache)-r.Cache)
			r.Out, r.Cache = max(out, r.Out), max(cr, r.Cache)
			return
		}
		fs.Recent = append(fs.Recent, recent{ID: id, Bucket: bucket, Family: fam, Out: out, Cache: cr})
		if len(fs.Recent) > recentIDs {
			fs.Recent = fs.Recent[len(fs.Recent)-recentIDs:]
		}
	}
	s.add(bucket, fam, out, cr)
	st.Counted++
}

func (s *Scanner) add(bucket int64, fam int, out, cr int64) {
	if out == 0 && cr == 0 {
		return
	}
	c := s.sums[bucket]
	if c == nil {
		c = make([]Counts, len(Families))
		s.sums[bucket] = c
	}
	c[fam].Out += out
	c[fam].CacheRead += cr
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
			if over {
				return nil, n, true
			}
			return buf, n, true
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		default:
			return nil, 0, false
		}
	}
}

// Buckets returns n buckets of step seconds from start, per family and kind:
// out[family][i] and cacheRead[family][i]. step is a multiple of BucketSec.
func (s *Scanner) Buckets(start, step int64, n int) (out, cacheRead [][]float64) {
	out = make([][]float64, len(Families))
	cacheRead = make([][]float64, len(Families))
	for f := range out {
		out[f], cacheRead[f] = make([]float64, n), make([]float64, n)
	}
	for b, v := range s.sums {
		i := int((b - start) / step)
		if b < start || i >= n {
			continue
		}
		for f := range Families {
			out[f][i] += float64(v[f].Out)
			cacheRead[f][i] += float64(v[f].CacheRead)
		}
	}
	return out, cacheRead
}

// cacheFile is what is kept on disk.
type cacheFile struct {
	Version int                   `json:"v"`
	Files   map[string]*fileState `json:"files"`
	Sums    map[int64][]Counts    `json:"sums"`
}

const cacheVersion = 1

func (s *Scanner) load() {
	if s.cache == "" {
		return
	}
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
	for b, v := range c.Sums {
		if len(v) == len(Families) {
			s.sums[b] = v
		}
	}
}

func (s *Scanner) save() {
	if s.cache == "" || !s.dirty {
		return
	}
	b, err := json.Marshal(cacheFile{Version: cacheVersion, Files: s.files, Sums: s.sums})
	if err != nil || os.MkdirAll(filepath.Dir(s.cache), 0o700) != nil {
		return
	}
	tmp := s.cache + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil && os.Rename(tmp, s.cache) == nil {
		s.dirty = false
	}
}
