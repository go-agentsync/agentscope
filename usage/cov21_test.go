package usage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/quota"
)

// A line whose content is a plain string, not a list of results, is not a
// refusal even when it carries the tool_result mark and a hook error: the
// mark is a JSON string elsewhere in the line, which is what lets it through
// the cheap byte test and into the decoder that must then refuse it.
func TestAContentThatIsNotAListOfResultsIsNotARefusal(t *testing.T) {
	root := t.TempDir()
	at := now.Add(-time.Hour)
	write(t, root, "p/a.jsonl",
		`{"type":"user","timestamp":"`+at.UTC().Format(time.RFC3339Nano)+`","kind":"tool_result","message":{"content":"a hook error"}}`+"\n")
	s := NewScanner(filepath.Join(root, "projects"), "")
	s.Refresh(now)
	if got := refs(s); len(got) != 0 {
		t.Fatalf("%v", got)
	}
}

func TestScanFileGivesUpOnAFileItCannotOpenOrSeek(t *testing.T) {
	s := NewScanner(t.TempDir(), "")
	st := &Stats{}
	fs := &fileState{}
	s.scanFile(filepath.Join(t.TempDir(), "absent.jsonl"), 0, fs, now.Add(-time.Hour), st)
	if st.Scanned != 0 {
		t.Fatalf("an absent file was scanned: %+v", st)
	}
	p := filepath.Join(t.TempDir(), "a.jsonl")
	if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.scanFile(p, 3, &fileState{Offset: -1}, now.Add(-time.Hour), st)
	if st.Scanned != 0 {
		t.Fatalf("a file that cannot be sought was scanned: %+v", st)
	}
}

// Only the last recentIDs message ids of a file are remembered for the dedup:
// the list is bounded however many answers a transcript holds.
func TestTheDedupMemoryOfAFileIsBounded(t *testing.T) {
	root := t.TempDir()
	var lines []string
	for i := 0; i < recentIDs+4; i++ {
		lines = append(lines, entry(now.Add(-time.Hour), fmt.Sprintf("m%d", i), "claude-opus-5-5", 5, 0))
	}
	write(t, root, "p/a.jsonl", lines...)
	s := NewScanner(filepath.Join(root, "projects"), "")
	s.Refresh(now)
	for _, fs := range s.files {
		if len(fs.Recent) != recentIDs || fs.Recent[len(fs.Recent)-1].ID != fmt.Sprintf("m%d", recentIDs+3) {
			t.Fatalf("%d ids kept, last %q", len(fs.Recent), fs.Recent[len(fs.Recent)-1].ID)
		}
	}
	if total(s, 0, "").Out != int64(5*(recentIDs+4)) {
		t.Fatalf("%d tokens", total(s, 0, "").Out)
	}
}

// A cache directory that cannot be made is a cache not written, not a failure
// of the scan.
func TestACacheThatCannotBeWrittenDoesNotFailTheScan(t *testing.T) {
	root := t.TempDir()
	write(t, root, "p/a.jsonl", owner("uuid-a"), entry(now.Add(-time.Hour), "m1", "claude-opus-5-5", 5, 0))
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewScanner(filepath.Join(root, "projects"), filepath.Join(file, "sub", "cache.json"))
	if st := s.Refresh(now); st.Counted != 1 {
		t.Fatalf("%+v", st)
	}
	if !s.dirty {
		t.Fatal("a cache that was not written was reported as saved")
	}
}

// history keeps only the readings of its window that were available and not
// older than the weeks shown, nor in the future.
func TestHistorySkipsWhatIsMissingUnavailableOrOutOfRange(t *testing.T) {
	const n = int64(10_000_000)
	label := quota.LabelWeek
	rs := []quota.Reading{
		{At: n - 10, Windows: []quota.Window{{Label: quota.LabelSession, Available: true}}},
		{At: n - 20, Windows: []quota.Window{{Label: label, Available: false}}},
		{At: n - (weeksShown*week + 1), Windows: []quota.Window{{Label: label, Available: true, Percent: 1}}},
		{At: n + 5, Windows: []quota.Window{{Label: label, Available: true, Percent: 2}}},
		{At: n - 30, Windows: []quota.Window{{Label: label, Available: true, Percent: 3}}},
	}
	got := history(rs, label, n, false)
	if len(got) != 1 || got[0].Percent != 3 {
		t.Fatalf("%v", got)
	}
}

func TestEngineRunStepsAgainOnEveryTick(t *testing.T) {
	old := scanEvery
	scanEvery = 5 * time.Millisecond
	defer func() { scanEvery = old }()
	e := NewEngine(t.TempDir(), t.TempDir(), t.TempDir(), func(context.Context) ([]byte, error) { return nil, errors.New("no") })
	var steps atomic.Int64
	e.now = func() time.Time { steps.Add(1); return now }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.Run(ctx); close(done) }()
	deadline := time.After(5 * time.Second)
	for steps.Load() < 3 { // the first step is Run's own, the others are ticks
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("Run never ticked")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	<-done
}
