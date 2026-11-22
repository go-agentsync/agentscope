package guard

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A recur that is found on PATH but cannot be started (here, a script whose
// interpreter does not exist) is an error that names recur; it is not an
// ExitError, which would be an answer.
func TestExecReportsARecurThatCannotBeStarted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shebang")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "recur"), []byte("#!/nonexistent/interpreter\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	out, err := Exec(context.Background(), t.TempDir())
	if err == nil || out != nil || !strings.HasPrefix(err.Error(), "recur: ") {
		t.Fatalf("out %q err %v", out, err)
	}
}
