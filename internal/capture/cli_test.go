package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// execCmd runs the command line with args, returning its output and error.
func execCmd(args ...string) (string, string, error) {
	cmd := RootCmd()
	var o, e bytes.Buffer
	cmd.SetOut(&o)
	cmd.SetErr(&e)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return o.String(), e.String(), err
}

func fast(t *testing.T) {
	t.Helper()
	old := portPoll
	portPoll = 20 * time.Millisecond
	t.Cleanup(func() { portPoll = old; dragSpec, evalExpr = "", "" })
}

func TestRootCmdWritesACapture(t *testing.T) {
	fast(t)
	t.Setenv("FAKE_CHROME", "ok")
	out := filepath.Join(t.TempDir(), "a", "shot.png")
	o, _, err := execCmd("--chrome", os.Args[0], "--out", out, "--wait", "1ms", "--eval", "1+1", "--width", "20", "--height", "10", "--dark")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(o, "eval:") || !strings.Contains(o, "wrote "+out) {
		t.Errorf("output %q", o)
	}
	if b, _ := os.ReadFile(out); string(b) != "PNG-BYTES" {
		t.Errorf("file %q", b)
	}
}

func TestRootCmdRefusals(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	for name, args := range map[string][]string{
		"no --out":          {},
		"a positional":      {"--out", filepath.Join(t.TempDir(), "x.png"), "extra"},
		"inside a git tree": {"--out", filepath.Join(root, "x.png"), "--chrome", "/no/chrome"},
		"unknown flag":      {"--out", "x", "--nope"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := execCmd(args...); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestMainReportsAnErrorAndExits(t *testing.T) {
	old := osExit
	defer func() { osExit = old }()
	code := -1
	osExit = func(c int) { code = c }
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"capture"} // --out missing
	main()
	if code != 1 {
		t.Fatalf("exit code %d", code)
	}
	code = -1
	os.Args = []string{"capture", "--help"}
	main()
	if code != -1 {
		t.Fatalf("--help exited %d", code)
	}
}

func TestRunRefusesBadClickAndDrag(t *testing.T) {
	fast(t)
	t.Setenv("FAKE_CHROME", "ok")
	out := filepath.Join(t.TempDir(), "x.png")
	err := run(context.Background(), os.Args[0], "http://127.0.0.1:1/", out, time.Millisecond, 0, 20, 10, "nonsense", false)
	if err == nil || !strings.Contains(err.Error(), "-click") {
		t.Errorf("click: %v", err)
	}
	dragSpec = "nonsense"
	err = run(context.Background(), os.Args[0], "http://127.0.0.1:1/", out, time.Millisecond, 0, 20, 10, "", false)
	if err == nil || !strings.Contains(err.Error(), "-drag") {
		t.Errorf("drag: %v", err)
	}
}

func TestRunNamesEveryRefusedStep(t *testing.T) {
	fast(t)
	for _, c := range []struct {
		name, method string
		click        string
		drag, eval   string
		scroll       int
		want         string
	}{
		{"metrics", "Emulation.setDeviceMetricsOverride", "", "", "", 0, "setDeviceMetricsOverride: boom"},
		{"media", "Emulation.setEmulatedMedia", "", "", "", 0, "setEmulatedMedia: boom"},
		{"click", "Input.dispatchMouseEvent", "1,2", "", "", 0, "dispatchMouseEvent: boom"},
		{"drag", "Input.dispatchMouseEvent", "", "1,2,3,4", "", 0, "dispatchMouseEvent: boom"},
		{"scroll", "Input.dispatchMouseEvent", "", "", "", 1, "dispatchMouseEvent: boom"},
		{"eval", "Runtime.evaluate", "", "", "1", 0, "evaluate: boom"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("FAKE_CHROME", "ok")
			t.Setenv("FAKE_FAIL", c.method)
			dragSpec, evalExpr = c.drag, c.eval
			out := filepath.Join(t.TempDir(), "x.png")
			err := run(context.Background(), os.Args[0], "http://127.0.0.1:1/", out, time.Millisecond, c.scroll, 20, 10, c.click, false)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestRunNamesAMalformedOrDroppedAnswer(t *testing.T) {
	fast(t)
	for _, c := range []struct{ name, env, method string }{
		{"createTarget result", "FAKE_BADRES", "Target.createTarget"},
		{"attachToTarget result", "FAKE_BADRES", "Target.attachToTarget"},
		{"screenshot result", "FAKE_BADRES", "Page.captureScreenshot"},
		{"dropped connection", "FAKE_DROP", "Target.createTarget"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("FAKE_CHROME", "ok")
			t.Setenv(c.env, c.method)
			out := filepath.Join(t.TempDir(), "x.png")
			if err := run(context.Background(), os.Args[0], "http://127.0.0.1:1/", out, time.Millisecond, 0, 20, 10, "", false); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestRunNamesAnUnwritableOutput(t *testing.T) {
	fast(t)
	t.Setenv("FAKE_CHROME", "ok")
	file := filepath.Join(t.TempDir(), "afile")
	os.WriteFile(file, nil, 0o644)
	err := run(context.Background(), os.Args[0], "http://127.0.0.1:1/", filepath.Join(file, "sub", "x.png"), time.Millisecond, 0, 20, 10, "", false)
	if err == nil {
		t.Fatal("accepted")
	}
}
