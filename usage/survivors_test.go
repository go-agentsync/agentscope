package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/quota"
)

// Each test below holds a refusal that a mutsweep of v0.9.0's code found nothing
// held.

func TestOnlyALineThatLooksLikeAnAnswerIsParsedAndCountedBad(t *testing.T) {
	root := t.TempDir()
	write(t, root, "p/a.jsonl",
		"this is not json and mentions nothing of interest\n", // never parsed: not an answer
		`{"type":"assistant","message":{"usage":`+"\n",        // looks like one, is not JSON
	)
	st := NewScanner(filepath.Join(root, "projects"), "").Refresh(now)
	if st.Bad != 1 {
		t.Fatalf("Bad = %d, want only the malformed answer", st.Bad)
	}
}

func TestAnAnswerOfNoTokensLeavesNoSum(t *testing.T) {
	root := t.TempDir()
	write(t, root, "p/a.jsonl", owner("uuid-a"), entry(now.Add(-time.Hour), "m1", "claude-opus-5-5", 0, 0))
	s := NewScanner(filepath.Join(root, "projects"), "")
	s.Refresh(now)
	if len(s.sums) != 0 {
		t.Fatalf("an answer of no tokens made %d sums: %v", len(s.sums), s.sums)
	}
	if keys, un := s.Accounts(); len(keys) != 0 || un {
		t.Fatalf("an account with no tokens is listed: %v %v", keys, un)
	}
}

func TestWithoutACacheNothingIsWrittenBesideTheProgram(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	root := t.TempDir()
	write(t, root, "p/a.jsonl", owner("uuid-a"), entry(now.Add(-time.Hour), "m1", "claude-opus-5-5", 5, 0))
	s := NewScanner(filepath.Join(root, "projects"), "")
	if st := s.Refresh(now); st.Counted != 1 {
		t.Fatalf("%+v", st)
	}
	ents, err := os.ReadDir(cwd)
	if err != nil || len(ents) != 0 {
		t.Fatalf("a scanner with no cache wrote %v (%v)", ents, err)
	}
}

func TestTheCacheIsWrittenOnlyWhenSomethingChanged(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(t.TempDir(), "sub", "cache.json")
	write(t, root, "p/a.jsonl", owner("uuid-a"), entry(now.Add(-time.Hour), "m1", "claude-opus-5-5", 5, 0))
	s := NewScanner(filepath.Join(root, "projects"), cache)
	s.Refresh(now)
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("a changed scan did not write its cache: %v", err)
	}
	os.Remove(cache)
	s.Refresh(now) // nothing appended: nothing to save
	if _, err := os.Stat(cache); err == nil {
		t.Fatal("an unchanged scan rewrote its cache")
	}
}

func TestLastAtOfNoReadingsIsZero(t *testing.T) {
	if got := lastAt(nil); got != 0 {
		t.Fatalf("%d", got)
	}
	if got := lastAt([]quota.Reading{{At: 5}, {At: 9}}); got != 9 {
		t.Fatalf("%d", got)
	}
}
