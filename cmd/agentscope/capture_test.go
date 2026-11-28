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

func TestCaptureWritesAWebM(t *testing.T) {
	out := filepath.Join(t.TempDir(), "sub", "demo.webm") // the extension alone names the format
	msg, err := runCapture(t, "--scenario", "theme-lang", "--size", "640x384", "--out", out)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	// An EBML header opens a WebM; the SVG writer would have written "<svg".
	if err != nil || !bytes.HasPrefix(b, []byte{0x1a, 0x45, 0xdf, 0xa3}) || !bytes.Contains(b, []byte("webm")) {
		t.Fatalf("not a WebM: %v", err)
	}
	if !strings.Contains(msg, "wrote "+out) || !strings.Contains(msg, "7 slots") || !strings.Contains(msg, "quantizer 10") || !strings.Contains(msg, "640x384") {
		t.Errorf("summary %q", msg)
	}
	out2 := filepath.Join(t.TempDir(), "q")
	if _, err := runCapture(t, "--scenario", "theme-lang", "--size", "640x384", "--format", "webm", "--quality", "30", "--out", out2); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(out2)
	if len(b2) >= len(b) {
		t.Errorf("quantizer 30 gave %d bytes, quantizer 10 gave %d", len(b2), len(b))
	}
}

func TestCaptureWebMDefaultOutKeepsTheFormat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	msg, err := runCapture(t, "--scenario", "theme-lang", "--size", "640x384", "--format", "webm")
	if err != nil || !strings.Contains(msg, filepath.Join("agentscope", "captures", "theme-lang.webm")) {
		t.Errorf("%v, summary %q", err, msg)
	}
}

func TestCaptureWebMRefusals(t *testing.T) {
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	dir := t.TempDir()
	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"a format against the extension": {[]string{"--format", "svg", "--out", filepath.Join(dir, "a.webm")}, "contradicts"},
		"a webm against an svg name":     {[]string{"--format", "webm", "--out", filepath.Join(dir, "a.svg")}, "contradicts"},
		"an unknown format":              {[]string{"--format", "gif"}, "want svg or webm"},
		"a quality on an svg":            {[]string{"--quality", "5", "--out", filepath.Join(dir, "a.svg")}, "webm only"},
		"a quality out of range":         {[]string{"--format", "webm", "--quality", "64"}, "--quality 64"},
		"an unaligned size":              {[]string{"--format", "webm", "--size", "1366x768"}, "not a multiple of 16"},
		"a git work tree":                {[]string{"--out", filepath.Join(repo, "x.webm"), "--size", "640x384"}, "refusing to write a capture inside a git work tree"},
	} {
		if msg, err := runCapture(t, c.args...); err == nil || !strings.Contains(err.Error()+msg, c.want) {
			t.Errorf("%s: err = %v, want it to say %q", name, err, c.want)
		}
	}
	for _, p := range []string{filepath.Join(repo, "x.webm"), filepath.Join(dir, "a.webm"), filepath.Join(dir, "a.svg")} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s was written despite the refusal", p)
		}
	}
}

func TestCaptureRefusals(t *testing.T) {
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"a bad size":       {[]string{"--size", "huge"}, "WIDTHxHEIGHT"},
		"an unknown name":  {[]string{"--scenario", "nope", "--out", filepath.Join(t.TempDir(), "x.svg")}, `unknown scenario "nope"`},
		"a git work tree":  {[]string{"--out", filepath.Join(repo, "x.svg")}, "refusing to write a capture inside a git work tree"},
		"a positional arg": {[]string{"extra"}, "unknown command"},
		"a tiny window":    {[]string{"--size", "10x10"}, "each 320 to 4096"},
	} {
		if msg, err := runCapture(t, c.args...); err == nil || !strings.Contains(err.Error()+msg, c.want) {
			t.Errorf("%s: err = %v, want it to say %q", name, err, c.want)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, "x.svg")); err == nil {
		t.Error("a capture was written inside a git work tree")
	}
}

func TestCaptureWithoutAHomeFails(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	_, err := runCapture(t, "--size", "320x320")
	if err == nil {
		t.Skip("this platform has a config directory without HOME")
	}
	// The error is the config directory's, not the work-tree refusal a relative
	// path in this repository would give if the guard were dropped.
	if !strings.Contains(err.Error(), "HOME") {
		t.Errorf("err = %v", err)
	}
}

func TestSanitize(t *testing.T) {
	if got := sanitize("../a/b\\c"); strings.ContainsAny(got, `/\`) || strings.Contains(got, "..") {
		t.Errorf("sanitize = %q", got)
	}
}
