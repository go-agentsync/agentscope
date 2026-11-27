package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// The tests drive run() against a Chrome that is this very test binary started
// again: TestMain sees FAKE_CHROME and serves the little of DevTools that run()
// speaks, instead of running tests. FAKE_CHROME says how it misbehaves.
func TestMain(m *testing.M) {
	if mode := os.Getenv("FAKE_CHROME"); mode != "" {
		fakeChrome(mode)
		return
	}
	os.Exit(m.Run())
}

func fakeChrome(mode string) {
	var prof string
	for _, a := range os.Args {
		if v, ok := strings.CutPrefix(a, "--user-data-dir="); ok {
			prof = v
		}
	}
	if mode == "noport" {
		select {}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(3)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "badjson":
			w.Write([]byte("nope"))
		case "badws":
			fmt.Fprint(w, `{"webSocketDebuggerUrl":"ws://127.0.0.1:1/x"}`)
		default:
			fmt.Fprintf(w, `{"webSocketDebuggerUrl":"ws://127.0.0.1:%s/cdp"}`, port)
		}
	})
	mux.HandleFunc("/cdp", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		shot := os.Getenv("FAKE_SHOT")
		if shot == "" {
			shot = base64.StdEncoding.EncodeToString([]byte("PNG-BYTES"))
		}
		for {
			var m msg
			if wsjson.Read(r.Context(), c, &m) != nil {
				return
			}
			reply := msg{ID: m.ID, Result: []byte(`{}`)}
			switch {
			case m.Method == os.Getenv("FAKE_DROP"):
				return
			case m.Method == os.Getenv("FAKE_BADRES"):
				reply.Result = []byte(`"not an object"`)
			case m.Method == os.Getenv("FAKE_FAIL"):
				reply.Result = nil
				reply.Error = &struct {
					Message string `json:"message"`
				}{"boom"}
			case m.Method == "Target.createTarget":
				reply.Result = []byte(`{"targetId":"t1"}`)
			case m.Method == "Target.attachToTarget":
				reply.Result = []byte(`{"sessionId":"s1"}`)
			case m.Method == "Page.captureScreenshot":
				reply.Result = []byte(`{"data":"` + shot + `"}`)
			}
			// A stray event first: call() must skip what is not its answer.
			wsjson.Write(r.Context(), c, msg{Method: "Page.loadEventFired"})
			if wsjson.Write(r.Context(), c, reply) != nil {
				return
			}
		}
	})
	file := port + "\n/devtools/browser/x"
	if mode == "badport" {
		file = "1\n"
	}
	os.WriteFile(filepath.Join(prof, "DevToolsActivePort"), []byte(file), 0o600)
	http.Serve(ln, mux)
}

// shoot runs run() against the fake Chrome in the given mode, and returns the
// error and the capture's path.
func shoot(t *testing.T, mode string, env map[string]string) (error, string) {
	t.Helper()
	t.Setenv("FAKE_CHROME", mode)
	for k, v := range env {
		t.Setenv(k, v)
	}
	old := portPoll
	portPoll = 20 * time.Millisecond
	t.Cleanup(func() { portPoll = old; dragSpec, evalExpr = "", "" })
	out := filepath.Join(t.TempDir(), "sub", "shot.png")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return run(ctx, os.Args[0], "http://127.0.0.1:1/", out, time.Millisecond, 0, 20, 10, "", false), out
}

func TestRunWritesWhatChromeCaptured(t *testing.T) {
	dragSpec, evalExpr = "1,2,30,4", "1+1"
	t.Setenv("FAKE_CHROME", "ok")
	old := portPoll
	portPoll = 20 * time.Millisecond
	defer func() { portPoll = old; dragSpec, evalExpr = "", "" }()
	out := filepath.Join(t.TempDir(), "sub", "shot.png")
	// A click, a scroll, a drag and an evaluation, all answered.
	if err := run(context.Background(), os.Args[0], "http://127.0.0.1:1/", out, time.Millisecond, 1, 20, 10, "5,6", true); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(out); err != nil || string(b) != "PNG-BYTES" {
		t.Fatalf("%q %v", b, err)
	}
}

func TestRunNamesWhatWentWrongBetweenItAndChrome(t *testing.T) {
	for _, c := range []struct {
		name, mode string
		env        map[string]string
		want       string
	}{
		{"a Chrome that never publishes its port", "noport", nil, "DevTools port"},
		{"a port nothing listens on", "badport", nil, "connect"},
		{"a version page that is not JSON", "badjson", nil, "invalid character"},
		{"a debugger URL nothing listens on", "badws", nil, "connect"},
		{"a refused Target.createTarget", "ok", map[string]string{"FAKE_FAIL": "Target.createTarget"}, "Target.createTarget: boom"},
		{"a refused Target.attachToTarget", "ok", map[string]string{"FAKE_FAIL": "Target.attachToTarget"}, "Target.attachToTarget: boom"},
		{"a refused screenshot", "ok", map[string]string{"FAKE_FAIL": "Page.captureScreenshot"}, "Page.captureScreenshot: boom"},
		{"a screenshot that is not base64", "ok", map[string]string{"FAKE_SHOT": "!!!"}, "base64"},
	} {
		t.Run(c.name, func(t *testing.T) { // one environment per case
			err, out := shoot(t, c.mode, c.env)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want one saying %q", err, c.want)
			}
			if _, serr := os.Stat(out); serr == nil {
				t.Error("a capture was written all the same")
			}
		})
	}
}

func TestRunNamesAnUnusableTemporaryDirectory(t *testing.T) {
	out := filepath.Join(t.TempDir(), "x.png")
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "absent"))
	err := run(context.Background(), os.Args[0], "http://127.0.0.1:1/", out, time.Millisecond, 0, 10, 10, "", false)
	if err == nil || !strings.Contains(err.Error(), "absent") {
		t.Fatalf("err = %v, want the temporary directory's own error", err)
	}
}
