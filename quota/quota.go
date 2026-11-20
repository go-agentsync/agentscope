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

// Reading is every window at one instant (Unix seconds).
type Reading struct {
	At      int64    `json:"at"`
	Windows []Window `json:"w"`
}

// statusJSON is the part of `aiquota status -o json` that is read.
type statusJSON struct {
	At      time.Time `json:"at"`
	Windows []struct {
		Label       string  `json:"label"`
		UsedPercent float64 `json:"used_percent"`
		ResetsAt    int64   `json:"resets_at_unix"`
		Verdict     string  `json:"verdict"`
	} `json:"windows"`
}

// Parse decodes the output of `aiquota status -o json`. A window whose verdict
// is "n/a" is Available false: it is listed, never drawn as 0.
func Parse(b []byte) (Reading, error) {
	var s statusJSON
	if err := json.Unmarshal(b, &s); err != nil {
		return Reading{}, fmt.Errorf("aiquota status: %w", err)
	}
	if s.At.IsZero() {
		return Reading{}, errors.New("aiquota status: no timestamp")
	}
	r := Reading{At: s.At.Unix()}
	for _, w := range s.Windows {
		if len(r.Windows) >= maxWindows {
			break
		}
		ok := w.Verdict != "n/a" && w.Verdict != ""
		win := Window{Label: clip(w.Label, 64), Available: ok}
		if ok {
			win.Percent, win.ResetsAt = clamp(w.UsedPercent), w.ResetsAt
		}
		r.Windows = append(r.Windows, win)
	}
	if len(r.Windows) == 0 {
		return Reading{}, errors.New("aiquota status: no windows")
	}
	return r, nil
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
