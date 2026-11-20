package main

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-agentsync/agentscope/quota"
)

func init() {
	// No test reads the real aiquota or writes the real history.
	quotaRunner = quota.Runner(func(context.Context) ([]byte, error) { return nil, errors.New("not in tests") })
}

func TestUsageDirsHonourTheFlagsAndTheUserDirectories(t *testing.T) {
	c, h, err := usageDirs("/c", "/h")
	if err != nil || c != "/c" || h != "/h" {
		t.Fatal(c, h, err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	c, h, err = usageDirs("", "")
	if err != nil || filepath.Base(c) != "agentscope" || filepath.Base(h) != "agentscope" {
		t.Fatal(c, h, err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if _, _, err := usageDirs("", ""); err == nil {
		t.Fatal("no cache directory is no error")
	}
	t.Setenv("XDG_CACHE_HOME", "/x")
	if _, _, err := usageDirs("", ""); err == nil && runtime.GOOS != "darwin" {
		t.Fatal("no config directory is no error")
	}
}
