package quota

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A history that cannot be rotated is an error, not a file that grows past its
// bound. The blocker is a non-empty directory where quota.jsonl.1 must go.
func TestStoreAppendRefusesWhenTheRotationFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rename semantics")
	}
	dir := t.TempDir()
	s := NewStore(dir)
	if err := os.WriteFile(s.path(), []byte(strings.Repeat("x", RotateBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(s.path()+".1", "keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Reading{At: 1}); err == nil {
		t.Fatal("a failed rotation was reported as success")
	}
}
