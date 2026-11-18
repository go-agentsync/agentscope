package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
)

// tree makes a minimal ~/.claude with one active session holding one lease.
func tree(t *testing.T) (claude string) {
	t.Helper()
	root := t.TempDir()
	claude = filepath.Join(root, ".claude")
	id := "aaaaaaaa-1111-4111-8111-111111111111"
	write := func(p, s string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(claude, "projects", "-p", id+".jsonl"), `{"type":"ai-title","aiTitle":"A title"}`+"\n")
	now := time.Now()
	b, _ := json.Marshal(map[string]any{"resource": "o/r", "owner": id, "pid": 1, "note": "a note", "acquired": now, "expires": now.Add(time.Hour)})
	write(filepath.Join(claude, "agentsync", "locks", "o_r.lease"), string(b))
	tell, _ := json.Marshal(map[string]any{"resource": "o/r", "from": id, "text": "hi", "at": now})
	write(filepath.Join(claude, "agentsync", "locks", "o_r.messages"), string(tell)+"\n")
	return claude
}

func run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errb bytes.Buffer
	cmd := RootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errb.String(), err
}

func TestSnapshotText(t *testing.T) {
	claude := tree(t)
	out, _, err := run(t, "snapshot", "--claude-dir", claude, "--localhost-root", "", "--github-root", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1 sessions (1 active", "SESSIONS", "aaaaaaaa ACTIVE", "a note", "LEASES (1 listed)", "held    aaaaaaaa", "TELLS (1)", "aaaaaaaa -> o/r: hi", "ANOMALIES (0)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestSnapshotTextShowsIdleAndNoTranscript(t *testing.T) {
	claude := tree(t)
	other := "bbbbbbbb-2222-4222-8222-222222222222"
	now := time.Now()
	b, _ := json.Marshal(map[string]any{"resource": "o/x", "owner": other, "acquired": now, "expires": now.Add(-time.Minute)})
	os.WriteFile(filepath.Join(claude, "agentsync", "locks", "o_x.lease"), b, 0o644)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(claude, "projects", "-p", "aaaaaaaa-1111-4111-8111-111111111111.jsonl"), old, old)
	out, _, err := run(t, "snapshot", "--claude-dir", claude, "--localhost-root", "", "--github-root", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "aaaaaaaa idle") || !strings.Contains(out, "expired bbbbbbbb") {
		t.Errorf("%s", out)
	}
}

func TestSnapshotJSONIsTheBrowsersMessage(t *testing.T) {
	claude := tree(t)
	out, _, err := run(t, "snapshot", "--json", "--claude-dir", claude, "--localhost-root", "", "--github-root", "")
	if err != nil {
		t.Fatal(err)
	}
	var s model.Snapshot
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatal(err)
	}
	if s.Version != model.Version || len(s.Sessions) != 1 || s.Sessions[0].Title != "A title" || len(s.Leases) != 1 {
		t.Errorf("%+v", s)
	}
}

func TestSnapshotStats(t *testing.T) {
	claude := tree(t)
	_, errOut, err := run(t, "snapshot", "--stats", "--warm", "2", "--claude-dir", claude, "--localhost-root", "", "--github-root", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "cold snapshot:") || !strings.Contains(errOut, "allocs") || !strings.Contains(errOut, "warm snapshot:") {
		t.Errorf("%q", errOut)
	}
}

func TestFlagValidation(t *testing.T) {
	claude := tree(t)
	for _, args := range [][]string{
		{"snapshot", "--claude-dir", claude, "--active", "0s"},
		{"snapshot", "--claude-dir", claude, "--horizon", "-1h"},
		{"snapshot", "--claude-dir", claude, "--max-agents", "-1"},
		{"snapshot", "extra"},
		{"serve", "--claude-dir", claude, "--active", "0s"},
		{"serve", "--claude-dir", claude, "--interval", "1ms"},
		{"serve", "--claude-dir", claude, "--web", filepath.Join(t.TempDir(), "absent")},
		{"serve", "--claude-dir", claude, "--web", t.TempDir(), "--addr", "0.0.0.0:0"},
	} {
		if _, _, err := run(t, args...); err == nil {
			t.Errorf("%v: no error", args)
		}
	}
}

func TestRootAndVersion(t *testing.T) {
	out, _, err := run(t)
	if err != nil || !strings.Contains(out, "snapshot") || !strings.Contains(out, "serve") {
		t.Errorf("%v %q", err, out)
	}
	out, _, err = run(t, "version")
	if err != nil || strings.TrimSpace(out) == "" {
		t.Errorf("%v %q", err, out)
	}
	if version() == "" {
		t.Error("version")
	}
}

func TestMainReportsErrorsThroughOsExit(t *testing.T) {
	oldExit, oldArgs, oldErr := osExit, os.Args, os.Stderr
	defer func() { osExit, os.Args, os.Stderr = oldExit, oldArgs, oldErr }()
	code := 0
	osExit = func(c int) { code = c }
	os.Args = []string{"agentscope", "snapshot", "--active", "0s"}
	r, w, _ := os.Pipe()
	os.Stderr = w
	main()
	w.Close()
	msg, _ := io.ReadAll(r)
	if code != 1 || !strings.Contains(string(msg), "positive") {
		t.Errorf("exit %d stderr %q", code, msg)
	}
	os.Args = []string{"agentscope", "version"}
	code = 0
	main()
	if code != 0 {
		t.Errorf("exit %d", code)
	}
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func TestServeServesUntilCancelled(t *testing.T) {
	claude := tree(t)
	web := t.TempDir()
	os.WriteFile(filepath.Join(web, "index.html"), []byte("<html>ui</html>"), 0o644)
	out := &lockedBuf{}
	cmd := RootCmd()
	cmd.SetOut(out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"serve", "--claude-dir", claude, "--localhost-root", "", "--github-root", "", "--web", web, "--addr", "127.0.0.1:0", "--interval", "500ms"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- cmd.ExecuteContext(ctx) }()

	var url string
	for i := 0; i < 200 && url == ""; i++ {
		if _, rest, ok := strings.Cut(out.String(), "serving "); ok {
			url, _, _ = strings.Cut(rest, " ")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if url == "" {
		t.Fatalf("no ready line: %q", out.String())
	}
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "ui") {
		t.Errorf("%d %q", resp.StatusCode, body)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop")
	}
}
