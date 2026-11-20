package accounts

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Each test below holds a refusal that a mutsweep of v0.9.0's code found nothing
// held: it fails when that refusal is deleted.

func TestParseAliasesNamesWhyItRefuses(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"an unterminated key is the key's fault", "\"abc\n", "unterminated"},
		{"a missing equals sign", "key \"v\"\n", "expected ="},
		{"a value that is not a string", "k = bad\n", "expected a string"},
		{"a value that opens no quote but closes one", "k = x\"\n", "expected a string"},
		{"no value at all", "\"k\" =\n", "expected a string"},
	} {
		m, err := parseAliases(strings.NewReader(c.in))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want one saying %q", c.name, err, c.want)
		}
		if len(m) != 0 {
			t.Errorf("%s: a refused file left aliases: %v", c.name, m)
		}
	}
}

func TestParseAliasesRefusesAFilePastItsBudgetOfComments(t *testing.T) {
	line := "#" + strings.Repeat("x", 1000) + "\n"
	big := strings.Repeat(line, maxAliasFile/1000+4)
	if _, err := parseAliases(strings.NewReader(big)); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("a file of %d bytes of comments: err = %v", len(big), err)
	}
	small := strings.Repeat(line, 3)
	if _, err := parseAliases(strings.NewReader(small)); err != nil {
		t.Fatalf("a small file of comments: %v", err)
	}
}

func TestLockReturnsAnUnexpectedErrorAtOnce(t *testing.T) {
	old := waitFor
	waitFor = 80 * time.Millisecond
	defer func() { waitFor = old }()
	start := time.Now()
	_, err := lock(filepath.Join(t.TempDir(), "no", "such", "dir", "x.lock"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want the create's own error, not the busy one", err)
	}
	if time.Since(start) >= waitFor {
		t.Fatalf("returned after %v: it waited for a lock that was never busy", time.Since(start))
	}
}

func TestAppendRefusesAStampItCannotEncode(t *testing.T) {
	s := NewStamps(t.TempDir())
	st := stamp(0, "h")
	st.At = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) // JSON years stop at 9999
	if wrote, err := s.Append(st); wrote || err == nil {
		t.Fatalf("wrote = %v, err = %v", wrote, err)
	}
	if _, err := os.Stat(s.path()); err == nil {
		t.Fatal("a refused stamp still created the file")
	}
}

func TestAppendReportsAnUnopenableStampFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "accounts.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	wrote, err := NewStamps(dir).Append(stamp(1, "h"))
	if wrote || err == nil || errors.Is(err, os.ErrInvalid) {
		t.Fatalf("wrote = %v, err = %v: want the open's own error", wrote, err)
	}
}
