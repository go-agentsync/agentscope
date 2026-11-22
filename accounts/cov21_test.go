package accounts

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A rotation that cannot be done refuses the stamp rather than writing past the
// bound. The blocker is a non-empty directory where accounts.jsonl.1 must go: a
// file cannot be renamed over it.
func TestStampsRefuseWhenTheRotationFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rename semantics")
	}
	dir := t.TempDir()
	s := NewStamps(dir)
	write(t, s.path(), strings.Repeat("x", RotateBytes))
	if err := os.MkdirAll(filepath.Join(s.path()+".1", "keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Append(stamp(1, "aaaa1111")); ok || err == nil {
		t.Fatalf("wrote %v, err %v: a failed rotation must be an error", ok, err)
	}
}

type failingTemp struct {
	name                     string
	chmodErr, writeErr, clos error
	closed                   int
}

func (f *failingTemp) Name() string                    { return f.name }
func (f *failingTemp) Chmod(os.FileMode) error         { return f.chmodErr }
func (f *failingTemp) WriteString(string) (int, error) { return 0, f.writeErr }
func (f *failingTemp) Close() error                    { f.closed++; return f.clos }

// Each step of the atomic write that can fail leaves the old file in place and
// reports the error; the steps that open a file close it.
func TestWriteAliasesReportsEachFailingStep(t *testing.T) {
	boom := errors.New("boom")
	old := createTemp
	defer func() { createTemp = old }()
	for name, ft := range map[string]*failingTemp{
		"chmod": {chmodErr: boom},
		"write": {writeErr: boom},
		"close": {clos: boom},
	} {
		dir := t.TempDir()
		ft.name = filepath.Join(dir, "tmp")
		createTemp = func(string, string) (tempFile, error) { return ft, nil }
		path := filepath.Join(dir, "accounts.toml")
		if err := writeAliases(path, map[string]string{"7567": "Labo"}); !errors.Is(err, boom) {
			t.Errorf("%s: err = %v", name, err)
		}
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s: the target was written", name)
		}
		if ft.closed == 0 {
			t.Errorf("%s: the temporary file was never closed", name)
		}
	}
	createTemp = func(string, string) (tempFile, error) { return nil, boom }
	if err := writeAliases(filepath.Join(t.TempDir(), "a.toml"), nil); !errors.Is(err, boom) {
		t.Errorf("create: %v", err)
	}
}
