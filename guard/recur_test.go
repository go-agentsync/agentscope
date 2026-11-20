package guard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary stand in for `recur`: Exec finds it on PATH.
func TestMain(m *testing.M) {
	switch os.Getenv("GUARD_FAKE_RECUR") {
	case "ok":
		os.Stdout.WriteString(realShaped)
		os.Exit(0)
	case "unaccounted": // recur exits 1 when a recurrence has no gate; the answer is still the answer
		os.Stdout.WriteString(realShaped)
		os.Exit(1)
	case "broken":
		os.Stdout.WriteString("usage: recur --memory <dir>\n")
		os.Exit(2)
	case "silent-fail":
		os.Exit(3)
	case "huge":
		os.Stdout.WriteString(strings.Repeat("x", maxOutput+1000))
		os.Exit(0)
	case "slow":
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// realShaped is what `recur --json` prints, cut down.
const realShaped = `{
  "lessons": [
    {"name": "a-lesson", "originSessionId": "", "modified": "2026-09-26T16:11:55Z",
     "recurrences": [{"date": "2026-09-26", "line": "Récidive le 2026-09-26"}, {"date": null, "line": "troisième fois"}, {"date": "2025-12-11", "yearInferred": true, "line": "le 11-12"}],
     "ledger": {"decision": "gate", "text": "replystamp --pretool", "writtenAt": "2026-11-13T17:56:00Z", "fingerprint": "ab"},
     "fingerprintMatches": true}
  ],
  "totals": {"lessons": 287, "decisions": {"gate": 50}, "recurrences": 653, "unreadable": 2},
  "unreadable": []
}`

// fakeRecur puts a `recur` on PATH that is this test binary, in the given mode.
func fakeRecur(t *testing.T, mode string) {
	t.Helper()
	bin := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(bin, "recur")); err != nil {
		t.Skip("cannot link the test binary:", err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("GUARD_FAKE_RECUR", mode)
}

func TestParseReadsWhatRecurPrintsAndRefusesWhatItDoesNot(t *testing.T) {
	r, err := Parse([]byte(realShaped))
	if err != nil || len(r.Lessons) != 1 || r.Totals.Lessons != 287 || r.Totals.Unreadable != 2 {
		t.Fatalf("%+v %v", r, err)
	}
	l := r.Lessons[0]
	if len(l.Recurrences) != 3 || l.Recurrences[1].Date != nil || !l.Recurrences[2].YearInferred || l.Ledger.Decision != "gate" || l.Ledger.WrittenAt == nil {
		t.Fatalf("%+v", l)
	}
	for name, in := range map[string]string{
		"a usage text":     "usage: recur --memory <dir>\n",
		"empty":            "",
		"no lessons key":   `{"totals": {}}`,
		"lessons mistyped": `{"lessons": 3}`,
	} {
		if r, err := Parse([]byte(in)); err == nil {
			t.Errorf("%s: parsed as %+v", name, r)
		}
	}
	// An empty ledger is an answer: zero lessons, measured.
	if r, err := Parse([]byte(`{"lessons": []}`)); err != nil || len(r.Lessons) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestExecRunsRecurAndKeepsTheAnswerOfAnExitStatusOne(t *testing.T) {
	for _, mode := range []string{"ok", "unaccounted"} {
		fakeRecur(t, mode)
		b, err := Exec(context.Background(), t.TempDir())
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if _, err := Parse(b); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
	}
}

func TestExecReportsWhatWentWrongInsteadOfAnEmptyAnswer(t *testing.T) {
	// No recur on PATH.
	t.Setenv("PATH", t.TempDir())
	if _, err := Exec(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("%v", err)
	}
	// It prints a usage text, or nothing, and fails: unparseable, so a failure above.
	for _, mode := range []string{"broken", "silent-fail"} {
		fakeRecur(t, mode)
		b, err := Exec(context.Background(), "x")
		if err == nil {
			_, err = Parse(b)
		}
		if err == nil {
			t.Fatalf("%s: read as an answer", mode)
		}
	}
	// Its output is bounded.
	fakeRecur(t, "huge")
	if b, err := Exec(context.Background(), "x"); err != nil || len(b) != maxOutput {
		t.Fatalf("%d bytes, %v", len(b), err)
	}
	// A recur that does not answer in time is a failure, not an old answer.
	fakeRecur(t, "slow")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := Exec(ctx, "x"); err == nil {
		t.Fatal("a recur that never answered was read")
	}
}

func TestMemoryDirTakesTheGivenOneElseTheLatestLedger(t *testing.T) {
	if got := MemoryDir("/given", "/nowhere"); got != "/given" {
		t.Fatal(got)
	}
	claude := t.TempDir()
	if got := MemoryDir("", claude); got != "" {
		t.Fatalf("no ledger anywhere: %q", got)
	}
	mk := func(proj string, age time.Duration) string {
		dir := filepath.Join(claude, "projects", proj, "memory")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		l := filepath.Join(dir, ".recur-ledger.md")
		if err := os.WriteFile(l, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-age)
		if err := os.Chtimes(l, at, at); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	older, newer := mk("a", 48*time.Hour), mk("b", time.Hour)
	if got := MemoryDir("", claude); got != newer {
		t.Fatalf("%q, want %q (not %q)", got, newer, older)
	}
	if err := os.MkdirAll(filepath.Join(claude, "projects", "c", "memory"), 0o700); err != nil { // a memory dir with no ledger
		t.Fatal(err)
	}
	if got := MemoryDir("", claude); got != newer {
		t.Fatalf("a directory without a ledger won: %q", got)
	}
}

func TestSourceReadsAtMostEveryTenMinutesAndSaysWhyItFailed(t *testing.T) {
	calls := 0
	fail := false
	s := &Source{Dir: "/m", Run: func(_ context.Context, dir string) ([]byte, error) {
		calls++
		if dir != "/m" {
			t.Fatal(dir)
		}
		if fail {
			return nil, errors.New("recur: boom")
		}
		return []byte(realShaped), nil
	}}
	t0 := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	if r, e := s.Get(context.Background(), t0); len(r.Lessons) != 1 || e != "" || calls != 1 {
		t.Fatalf("%v %q %d", r, e, calls)
	}
	if r, e := s.Get(context.Background(), t0.Add(MinInterval-time.Second)); len(r.Lessons) != 1 || e != "" || calls != 1 {
		t.Fatalf("read again within %s: %d calls", MinInterval, calls)
	}
	fail = true
	// A failure after a success is a failure: the old answer is not shown as current.
	if r, e := s.Get(context.Background(), t0.Add(MinInterval)); len(r.Lessons) != 0 || e != "recur: boom" || calls != 2 {
		t.Fatalf("%v %q %d", r, e, calls)
	}
	// And the failure is kept for the interval too: recur is not hammered.
	if r, e := s.Get(context.Background(), t0.Add(MinInterval+time.Minute)); len(r.Lessons) != 0 || e != "recur: boom" || calls != 2 {
		t.Fatalf("%v %q %d", r, e, calls)
	}
	// An answer that does not parse is a failure.
	bad := &Source{Dir: "/m", Run: func(context.Context, string) ([]byte, error) { return []byte("usage"), nil }}
	if r, e := bad.Get(context.Background(), t0); len(r.Lessons) != 0 || !strings.Contains(e, "recur --json") {
		t.Fatalf("%v %q", r, e)
	}
	// No memory directory: nothing is run.
	none := &Source{Run: func(context.Context, string) ([]byte, error) { t.Fatal("ran"); return nil, nil }}
	if r, e := none.Get(context.Background(), t0); len(r.Lessons) != 0 || !strings.Contains(e, "--memory-dir") {
		t.Fatalf("%v %q", r, e)
	}
}
