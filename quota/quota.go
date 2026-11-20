// Package quota reads the official usage windows from `aiquota status -o json`
// and keeps a bounded, append-only history of them.
//
// aiquota already shares one reading across processes and serves stale data
// while it is rate-limited, and keeps only its last reading; this package adds
// the history. It samples at most every MinInterval and never talks to a
// provider itself.
package quota

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

// Labels aiquota gives the two windows that have a reset the charts align on.
const (
	LabelSession = "Session (5hr)"
	LabelWeek    = "Weekly (7 day)"
)

// MinInterval is the shortest time between two readings.
const MinInterval = 5 * time.Minute

// Bounds on what is read and kept.
const (
	maxStatus   = 1 << 20 // bytes of `aiquota status` output taken
	maxWindows  = 32      // windows kept per reading
	maxLine     = 64 << 10
	RotateBytes = 4 << 20 // the history file is rotated past this size
)

// Window is one window of a reading.
type Window struct {
	Label     string  `json:"l"`
	Percent   float64 `json:"p,omitempty"`
	ResetsAt  int64   `json:"r,omitempty"`
	Available bool    `json:"a,omitempty"`
}

// Reading is every window of one account at one instant (Unix seconds).
//
// Acct is the account's stable key (see AccountKey, LabelKey); "" is a reading
// recorded before accounts were told apart, or taken when the account could not
// be told: its account is unknown and is never guessed.
type Reading struct {
	At      int64    `json:"at"`
	Acct    string   `json:"acct,omitempty"`
	Label   string   `json:"lbl,omitempty"`
	Plan    string   `json:"plan,omitempty"`
	Windows []Window `json:"w"`
}

// AccountKey is the stable, short key of an account id: the first 8 hex digits
// of its SHA-256. The id itself, and the e-mail, are never kept.
func AccountKey(id string) string {
	if id == "" {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(id)))[:8]
}

// LabelKey is the key of an account known only by the label aiquota gave it.
func LabelKey(label string) string { return "l" + AccountKey("label:" + label)[:7] }

// statusJSON is the part of `aiquota status -o json` that is read.
type statusJSON struct {
	At      time.Time `json:"at"`
	Windows []struct {
		Account     string  `json:"account"`
		Plan        string  `json:"plan"`
		Label       string  `json:"label"`
		UsedPercent float64 `json:"used_percent"`
		ResetsAt    int64   `json:"resets_at_unix"`
		Verdict     string  `json:"verdict"`
	} `json:"windows"`
}

// Parse decodes the output of `aiquota status -o json`: one Reading per
// account label, in order of appearance, their keys unset (see Tag). A window
// whose verdict is "n/a" is Available false: it is listed, never drawn as 0.
func Parse(b []byte) ([]Reading, error) {
	var s statusJSON
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("aiquota status: %w", err)
	}
	if s.At.IsZero() {
		return nil, errors.New("aiquota status: no timestamp")
	}
	var rs []Reading
	idx := map[string]int{}
	n := 0
	for _, w := range s.Windows {
		if n >= maxWindows {
			break
		}
		n++
		i, ok := idx[w.Account]
		if !ok {
			i = len(rs)
			idx[w.Account] = i
			rs = append(rs, Reading{At: s.At.Unix(), Label: clip(w.Account, 64), Plan: clip(w.Plan, 32)})
		}
		ok = w.Verdict != "n/a" && w.Verdict != ""
		win := Window{Label: clip(w.Label, 64), Available: ok}
		if ok {
			win.Percent, win.ResetsAt = clamp(w.UsedPercent), w.ResetsAt
		}
		rs[i].Windows = append(rs[i].Windows, win)
	}
	if len(rs) == 0 {
		return nil, errors.New("aiquota status: no windows")
	}
	return rs, nil
}

// Tag sets the accounts' keys. active is the key of the account Claude Code is
// signed in to ("" when it could not be told, or changed during the reading):
// with a single account in the output it is that one; with several, each is
// known by its label alone, since nothing says which of them is the active one.
func Tag(rs []Reading, active string) {
	for i := range rs {
		switch {
		case len(rs) == 1 && active != "":
			rs[i].Acct = active
		case len(rs) > 1:
			rs[i].Acct = LabelKey(rs[i].Label)
		default:
			rs[i].Acct = ""
		}
	}
}

func clamp(p float64) float64 {
	switch {
	case p != p, p < 0: // NaN, negative
		return 0
	case p > 100:
		return 100
	}
	return p
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// Find returns the window with the label.
func (r Reading) Find(label string) (Window, bool) {
	for _, w := range r.Windows {
		if w.Label == label {
			return w, true
		}
	}
	return Window{}, false
}

// Runner runs `aiquota status -o json` and returns its output.
type Runner func(ctx context.Context) ([]byte, error)

// Exec is the Runner that runs the aiquota binary on PATH, with fixed
// arguments and no shell.
func Exec(ctx context.Context) ([]byte, error) {
	path, err := exec.LookPath("aiquota")
	if err != nil {
		return nil, fmt.Errorf("aiquota not found: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "status", "-o", "json")
	var out bytes.Buffer
	cmd.Stdout = &limitWriter{w: &out, left: maxStatus}
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("aiquota status: %w", err)
	}
	return out.Bytes(), nil
}

// limitWriter drops what is written past its limit.
type limitWriter struct {
	w    io.Writer
	left int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > l.left {
		p = p[:l.left]
	}
	l.left -= len(p)
	_, err := l.w.Write(p)
	return n, err
}

// Store is the history: JSON lines in dir/quota.jsonl, rotated to quota.jsonl.1
// past RotateBytes (the older half of the history is the rotated file).
type Store struct {
	dir string
}

// NewStore keeps the history in dir, which is created when first written.
func NewStore(dir string) *Store { return &Store{dir: dir} }

func (s *Store) path() string { return filepath.Join(s.dir, "quota.jsonl") }

// Append adds one reading.
func (s *Store) Append(r Reading) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	if fi, err := os.Stat(s.path()); err == nil && fi.Size() >= RotateBytes {
		if err := os.Rename(s.path(), s.path()+".1"); err != nil {
			return err
		}
	}
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Load returns every reading kept, oldest first. A line that does not parse is
// skipped and counted: a torn write or a hand edit must not hide the rest.
func (s *Store) Load() (rs []Reading, bad int) {
	for _, p := range []string{s.path() + ".1", s.path()} {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		br := bufio.NewReaderSize(io.LimitReader(f, 2*RotateBytes), 64<<10)
		for {
			line, err := br.ReadSlice('\n')
			if len(line) > 0 && !errors.Is(err, bufio.ErrBufferFull) {
				var r Reading
				if json.Unmarshal(bytes.TrimSpace(line), &r) != nil || r.At <= 0 {
					bad++
				} else {
					rs = append(rs, r)
				}
			}
			if errors.Is(err, bufio.ErrBufferFull) { // a line past the buffer is garbage
				bad++
				for errors.Is(err, bufio.ErrBufferFull) {
					_, err = br.ReadSlice('\n')
				}
			}
			if err != nil {
				break
			}
		}
		f.Close()
	}
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].At < rs[j].At })
	return rs, bad
}
