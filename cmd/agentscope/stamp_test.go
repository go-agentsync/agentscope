package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/accounts"
)

func runStamp(t *testing.T, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errb bytes.Buffer
	cmd := RootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(append([]string{"stamp"}, args...))
	err = cmd.Execute()
	return out.String(), errb.String(), err
}

func claudeFile(t *testing.T, uuid, orgType string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".claude.json")
	body := `{"oauthAccount":{"accountUuid":"` + uuid + `","emailAddress":"x@example.org","organizationType":"` + orgType + `"}}`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// stamp appends only when the account changed, prints nothing, and keeps the
// session id and the hook's timestamp.
func TestStampAppendsOnlyOnChangeAndPrintsNothing(t *testing.T) {
	dir := t.TempDir()
	a, b := claudeFile(t, "uuid-A", "claude_team"), claudeFile(t, "uuid-B", "claude_max")
	steps := []struct {
		file string
		want int
	}{{a, 1}, {a, 1}, {b, 2}, {b, 2}, {a, 3}}
	for i, s := range steps {
		out, errs, err := runStamp(t, `{"session_id":"sess-1","timestamp":"2026-11-20T10:00:0`+string(rune('0'+i))+`Z","hook_event_name":"UserPromptSubmit"}`, "--claude-json", s.file, "--dir", dir)
		if err != nil || out != "" || errs != "" {
			t.Fatalf("step %d: stdout %q stderr %q err %v: a hook's stdout reaches the model", i, out, errs, err)
		}
		if got := accounts.NewStamps(dir).Load().Len(); got != s.want {
			t.Fatalf("step %d: %d stamps, want %d", i, got, s.want)
		}
	}
	st, ok := accounts.NewStamps(dir).Load().At(time.Date(2026, 11, 20, 10, 0, 2, 0, time.UTC))
	if !ok || st.SessionID != "sess-1" || st.Plan != "max" || st.Label != "Claude Code (max)" || st.AccountHash == "" {
		t.Fatalf("%+v %v", st, ok)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "accounts.jsonl")); strings.Contains(string(b), "example.org") || strings.Contains(string(b), "uuid-") {
		t.Fatal("the stamp file holds an e-mail or an id")
	}
}

func TestStampWithAnUnreadableAccountFileWritesNothingAndExitsZero(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte("{not json"), 0o600)
	for _, f := range []string{filepath.Join(t.TempDir(), "missing.json"), bad} {
		out, errs, err := runStamp(t, `{"session_id":"s"}`, "--claude-json", f, "--dir", dir)
		if err != nil || out != "" || errs != "" {
			t.Fatalf("%q %q %v", out, errs, err)
		}
		// --verbose says why, on stderr only.
		out, errs, err = runStamp(t, `{}`, "--claude-json", f, "--dir", dir, "--verbose")
		if err != nil || out != "" || !strings.Contains(errs, "no signed-in account") {
			t.Fatalf("%q %q %v", out, errs, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "accounts.jsonl")); err == nil {
		t.Fatal("a stamp was written for an account that could not be read")
	}
}

// A payload that is not JSON, or has no timestamp, still stamps (now); an
// overlong session id is cut.
func TestStampToleratesAnOddPayload(t *testing.T) {
	dir := t.TempDir()
	stampNow = func() time.Time { return time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC) }
	defer func() { stampNow = time.Now }()
	f := claudeFile(t, "u", "")
	if _, _, err := runStamp(t, "not json at all", "--claude-json", f, "--dir", dir); err != nil {
		t.Fatal(err)
	}
	st, ok := accounts.NewStamps(dir).Load().At(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	if !ok || st.Plan != "" || st.Label != "Claude Code" {
		t.Fatalf("%+v %v", st, ok)
	}
	long := strings.Repeat("s", 200)
	f2 := claudeFile(t, "u2", "")
	if _, _, err := runStamp(t, `{"session_id":"`+long+`"}`, "--claude-json", f2, "--dir", dir); err != nil {
		t.Fatal(err)
	}
	if st, _ := accounts.NewStamps(dir).Load().At(time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)); len(st.SessionID) != 64 {
		t.Fatalf("session id %d bytes", len(st.SessionID))
	}
}

// Sessions run the hook at the same instant: the same account is written once.
func TestStampConcurrentHooks(t *testing.T) {
	dir := t.TempDir()
	f := claudeFile(t, "same", "claude_team")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stamp(strings.NewReader(`{"session_id":"s"}`), f, dir)
		}()
	}
	wg.Wait()
	if n := accounts.NewStamps(dir).Load().Len(); n != 1 {
		t.Fatalf("%d stamps from 20 concurrent hooks of one account", n)
	}
}

func TestStampDefaultDirAndErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	f := claudeFile(t, "u", "")
	if why := stamp(strings.NewReader("{}"), f, ""); why != "" {
		t.Fatal(why)
	}
	base, _ := os.UserConfigDir()
	if accounts.NewStamps(filepath.Join(base, "agentscope")).Load().Len() != 1 {
		t.Fatal("the default directory is <user config dir>/agentscope")
	}
	// A directory that cannot be made reports why.
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, []byte("x"), 0o600)
	if why := stamp(strings.NewReader("{}"), f, filepath.Join(file, "sub")); why == "" {
		t.Fatal("an unwritable directory is reported")
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if why := stamp(strings.NewReader("{}"), f, ""); why == "" {
		t.Fatal("no config directory is reported")
	}
}
