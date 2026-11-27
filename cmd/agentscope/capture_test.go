package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCapture(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := RootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"capture"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestCaptureListsTheScenarios(t *testing.T) {
	out, err := runCapture(t, "--list")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"overview", "colors", "theme-lang"} {
		if !strings.Contains(out, n+"\t") {
			t.Errorf("%s is not listed: %q", n, out)
		}
	}
}

func TestCaptureWritesAnSVG(t *testing.T) {
	out := filepath.Join(t.TempDir(), "sub", "demo.svg")
	msg, err := runCapture(t, "--scenario", "theme-lang", "--size", "640x400", "--out", out)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil || !bytes.Contains(b, []byte("<svg")) || !bytes.Contains(b, []byte("@keyframes")) {
		t.Fatalf("not an animated SVG: %v", err)
	}
	if !strings.Contains(msg, "wrote "+out) || !strings.Contains(msg, "7 frames") {
		t.Errorf("summary %q", msg)
	}
}

func TestCaptureDefaultOutIsUnderTheConfigDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	msg, err := runCapture(t, "--scenario", "overview", "--size", "320x320")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, home) || !strings.Contains(msg, filepath.Join("agentscope", "captures", "overview.svg")) {
		t.Errorf("summary %q", msg)
	}
}

func TestCaptureRefusals(t *testing.T) {
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	for name, args := range map[string][]string{
		"a bad size":       {"--size", "huge"},
		"an unknown name":  {"--scenario", "nope", "--out", filepath.Join(t.TempDir(), "x.svg")},
		"a git work tree":  {"--out", filepath.Join(repo, "x.svg")},
		"a positional arg": {"extra"},
		"a tiny window":    {"--size", "10x10"},
	} {
		if _, err := runCapture(t, args...); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, "x.svg")); err == nil {
		t.Error("a capture was written inside a git work tree")
	}
}

func TestCaptureWithoutAHomeFails(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if _, err := runCapture(t, "--size", "320x320"); err == nil {
		t.Skip("this platform has a config directory without HOME")
	}
}

func TestSanitize(t *testing.T) {
	if got := sanitize("../a/b\\c"); strings.ContainsAny(got, `/\`) || strings.Contains(got, "..") {
		t.Errorf("sanitize = %q", got)
	}
}
