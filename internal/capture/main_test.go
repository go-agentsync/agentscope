package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInsideGitTree(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "repo", ".git"), 0o755)
	os.MkdirAll(filepath.Join(root, "repo", "sub", "deep"), 0o755)
	os.MkdirAll(filepath.Join(root, "plain"), 0o755)
	for p, want := range map[string]bool{
		filepath.Join(root, "repo", "a.png"):            true,
		filepath.Join(root, "repo", "sub", "deep", "x"): true,
		filepath.Join(root, "plain", "a.png"):           false,
		filepath.Join(root, "not-yet-created", "a.png"): false,
	} {
		got, err := insideGitTree(p)
		if err != nil || got != want {
			t.Errorf("insideGitTree(%q) = %v, %v; want %v", p, got, err, want)
		}
	}
}

func TestRunRefusesAGitTreeBeforeStartingAnything(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	err := run(context.Background(), "/definitely/not/chrome", "http://127.0.0.1:1/", filepath.Join(root, "x.png"), time.Second, 0, 10, 10, "", false)
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("want a refusal, got %v", err)
	}
	if _, serr := os.Stat(filepath.Join(root, "x.png")); serr == nil {
		t.Fatal("a capture was written inside a git work tree")
	}
}

func TestRunWithAMissingChrome(t *testing.T) {
	out := filepath.Join(t.TempDir(), "x.png")
	if err := run(context.Background(), "/definitely/not/chrome", "http://127.0.0.1:1/", out, time.Second, 0, 10, 10, "", true); err == nil {
		t.Fatal("a missing Chrome was accepted")
	}
}
