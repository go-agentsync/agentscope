package guard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Recurrence is `recur --memory <dir> --json`: the lessons that recorded a
// recurrence, what the ledger decided about each, and the totals. Only the fields
// the tab shows are decoded.
type Recurrence struct {
	Lessons []Lesson `json:"lessons"`
	Totals  struct {
		Lessons    int `json:"lessons"`
		Unreadable int `json:"unreadable"`
	} `json:"totals"`
}

// Lesson is one memory with its recurrences and ledger entry.
type Lesson struct {
	Name        string `json:"name"`
	Recurrences []struct {
		Date         *string `json:"date"`
		YearInferred bool    `json:"yearInferred"`
		Line         string  `json:"line"`
	} `json:"recurrences"`
	Ledger struct {
		Decision  string  `json:"decision"`
		Text      string  `json:"text"`
		WrittenAt *string `json:"writtenAt"`
	} `json:"ledger"`
}

// Parse decodes recur's JSON; it refuses what has no "lessons" key, so an answer
// that is not recur's (an error page, a usage text) is a failure and not an empty
// list.
func Parse(b []byte) (*Recurrence, error) {
	var probe struct {
		Lessons *json.RawMessage `json:"lessons"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return nil, fmt.Errorf("recur --json: %w", err)
	}
	if probe.Lessons == nil {
		return nil, errors.New("recur --json: no \"lessons\" in the answer")
	}
	var r Recurrence
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("recur --json: %w", err)
	}
	return &r, nil
}

// Runner runs recur on a memory directory and returns its standard output. A
// non-zero exit status is not an error here when the output parses: recur exits 1
// to report an unaccounted recurrence, and that is an answer.
type Runner func(ctx context.Context, memDir string) ([]byte, error)

// maxOutput bounds what is read from recur.
const maxOutput = 32 << 20

// Exec is the Runner that runs the recur binary on PATH, with fixed arguments and
// no shell, under a timeout.
func Exec(ctx context.Context, memDir string) ([]byte, error) {
	path, err := exec.LookPath("recur")
	if err != nil {
		return nil, fmt.Errorf("recur not found: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--memory", memDir, "--json")
	var out bytes.Buffer
	cmd.Stdout = &limited{w: &out, left: maxOutput}
	err = cmd.Run()
	var ee *exec.ExitError
	if err != nil && !errors.As(err, &ee) {
		return nil, fmt.Errorf("recur: %w", err)
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("recur: %w", ctx.Err())
	}
	return out.Bytes(), nil
}

type limited struct {
	w    *bytes.Buffer
	left int
}

func (l *limited) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > l.left {
		p = p[:l.left]
	}
	l.left -= len(p)
	l.w.Write(p)
	return n, nil
}

// MemoryDir finds the memory directory to read: the one given, else the one under
// claudeDir/projects whose recur ledger was written last. "" when there is none.
func MemoryDir(given, claudeDir string) string {
	if given != "" {
		return given
	}
	ledgers, _ := filepath.Glob(filepath.Join(claudeDir, "projects", "*", "memory", ".recur-ledger.md"))
	best, bestAt := "", time.Time{}
	for _, l := range ledgers {
		if fi, err := os.Stat(l); err == nil && (best == "" || fi.ModTime().After(bestAt)) {
			best, bestAt = filepath.Dir(l), fi.ModTime()
		}
	}
	return best
}

// MinInterval is how seldom recur is run: its answer is a reading of memory files.
const MinInterval = 10 * time.Minute

// Source reads recur at most every MinInterval and keeps the last answer. A
// failure is kept as the reason, never as an empty answer, so the tab says "not
// measured" and not "0".
type Source struct {
	Dir string // "" means no memory directory could be found
	Run Runner

	mu   sync.Mutex
	at   time.Time
	rec  *Recurrence
	err  string
	read bool
}

// Get is the last answer, running recur first when it has not been read for
// MinInterval. err is "" when rec is the answer.
func (s *Source) Get(ctx context.Context, now time.Time) (rec *Recurrence, err string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.read && now.Sub(s.at) < MinInterval {
		return s.rec, s.err
	}
	s.read, s.at = true, now
	s.rec, s.err = nil, ""
	if s.Dir == "" {
		s.err = "no recur ledger found under the Claude directory (pass --memory-dir)"
		return nil, s.err
	}
	b, e := s.Run(ctx, s.Dir)
	if e == nil {
		s.rec, e = Parse(b)
	}
	if e != nil {
		s.rec, s.err = nil, e.Error()
	}
	return s.rec, s.err
}
