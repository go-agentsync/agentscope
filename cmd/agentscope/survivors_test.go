package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// serveRefusing runs `serve` where it must refuse before serving. It can neither
// reach the default address (it passes --addr 127.0.0.1:0) nor run for long: a
// refusal that is missing makes the server run until the context ends, which the
// test reads as "no error". That is what lets a mutation that deletes the
// refusal fail this test instead of hanging the suite on a live server.
func serveRefusing(t *testing.T, extra ...string) error {
	t.Helper()
	web := t.TempDir()
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<html>ui</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := RootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(append([]string{"serve", "--claude-dir", tree(t), "--localhost-root", "", "--github-root",
		"", "--addr", "127.0.0.1:0", "--web", web, "--cache-dir", t.TempDir(), "--history-dir", t.TempDir()}, extra...))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return cmd.ExecuteContext(ctx)
}

func TestServeRefusesWhatItMustNotServe(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"an interval below 500ms", []string{"--interval", "100ms"}, "at least 500ms"},
		{"a zero active window", []string{"--active", "0s"}, "positive"},
		{"a zero horizon", []string{"--horizon", "0s"}, "positive"},
		{"a negative tell window", []string{"--tell-window", "-1s"}, "not negative"},
		{"a negative sub-agent count", []string{"--max-agents", "-1"}, "not negative"},
		{"an address that is not loopback", []string{"--addr", "0.0.0.0:0"}, ""},
	} {
		err := serveRefusing(t, c.args...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want one saying %q", c.name, err, c.want)
		}
	}
}

func TestServeStopsWhereTheUsageDirectoriesCannotBeFound(t *testing.T) {
	boom := errors.New("no user directory")
	oldCache, oldConfig := userCacheDir, userConfigDir
	defer func() { userCacheDir, userConfigDir = oldCache, oldConfig }()
	userCacheDir = func() (string, error) { return "", boom }
	userConfigDir = func() (string, error) { return "", boom }
	web := t.TempDir()
	os.WriteFile(filepath.Join(web, "index.html"), []byte("<html>ui</html>"), 0o644)
	cmd := RootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	// No --cache-dir and no --history-dir: both come from the user directories.
	cmd.SetArgs([]string{"serve", "--claude-dir", tree(t), "--localhost-root", "", "--github-root", "",
		"--addr", "127.0.0.1:0", "--web", web})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := cmd.ExecuteContext(ctx); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the directory lookup's own error", err)
	}
}

func TestSnapshotRefusesANegativeTellWindowAndAcceptsZero(t *testing.T) {
	claude := tree(t)
	base := []string{"snapshot", "--claude-dir", claude, "--localhost-root", "", "--github-root", ""}
	if _, _, err := run(t, append(base, "--tell-window", "-1s")...); err == nil {
		t.Error("a negative tell window was accepted")
	}
	if _, _, err := run(t, append(base, "--tell-window", "0s", "--max-agents", "0")...); err != nil {
		t.Errorf("zero is a valid tell window and sub-agent count: %v", err)
	}
}
