// Command capture takes a screenshot of a running agentscope page with a real
// Chrome, after the wasm UI has had time to receive its stream, and can scroll
// the graph first. Headless Chrome's own --screenshot fires at the load event,
// before the wasm has booted, so it shoots an empty window.
//
//	go run ./internal/capture -chrome "<path>" -url http://127.0.0.1:8765/ \
//	    -out ~/Library/Application\ Support/agentscope/captures/live.png -wait 6s -scroll 0
//
// A capture shows real session ids and lease notes, so it is refused when -out
// is inside a git work tree (any parent directory holds a .git).
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// insideGitTree reports whether p, or a parent of it, holds a .git.
func insideGitTree(p string) (bool, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return false, err
	}
	for d := filepath.Dir(abs); ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return true, nil
		}
		if d == filepath.Dir(d) {
			return false, nil
		}
	}
}

type msg struct {
	ID     int             `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params any             `json:"params,omitempty"`
	Sess   string          `json:"sessionId,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type cdp struct {
	c    *websocket.Conn
	next int
}

// call sends one command and waits for its answer, ignoring events.
func (d *cdp) call(ctx context.Context, sess, method string, params any) (json.RawMessage, error) {
	d.next++
	id := d.next
	if err := wsjson.Write(ctx, d.c, msg{ID: id, Method: method, Params: params, Sess: sess}); err != nil {
		return nil, err
	}
	for {
		var m msg
		if err := wsjson.Read(ctx, d.c, &m); err != nil {
			return nil, err
		}
		if m.ID != id {
			continue
		}
		if m.Error != nil {
			return nil, errors.New(method + ": " + m.Error.Message)
		}
		return m.Result, nil
	}
}

func run(ctx context.Context, chrome, url, out string, wait time.Duration, scroll, w, h int) error {
	if in, err := insideGitTree(out); err != nil || in {
		return fmt.Errorf("refusing to write a capture inside a git work tree (%s): %v", out, err)
	}
	prof, err := os.MkdirTemp("", "agentscope-capture-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(prof)
	cmd := exec.CommandContext(ctx, chrome, "--headless=new", "--disable-gpu", "--hide-scrollbars",
		"--remote-debugging-port=0", "--user-data-dir="+prof, fmt.Sprintf("--window-size=%d,%d", w, h), "about:blank")
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	var port string
	for i := 0; i < 100 && port == ""; i++ {
		if b, err := os.ReadFile(filepath.Join(prof, "DevToolsActivePort")); err == nil {
			port, _, _ = strings.Cut(string(b), "\n")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if port == "" {
		return errors.New("Chrome did not publish its DevTools port")
	}
	resp, err := http.Get("http://127.0.0.1:" + port + "/json/version")
	if err != nil {
		return err
	}
	var ver struct {
		WS string `json:"webSocketDebuggerUrl"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&ver)
	resp.Body.Close()
	if err != nil {
		return err
	}
	c, _, err := websocket.Dial(ctx, ver.WS, nil)
	if err != nil {
		return err
	}
	defer c.CloseNow()
	c.SetReadLimit(64 << 20)
	d := &cdp{c: c}

	raw, err := d.call(ctx, "", "Target.createTarget", map[string]any{"url": url})
	if err != nil {
		return err
	}
	var tgt struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(raw, &tgt); err != nil {
		return err
	}
	raw, err = d.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": tgt.TargetID, "flatten": true})
	if err != nil {
		return err
	}
	var att struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &att); err != nil {
		return err
	}
	s := att.SessionID
	if _, err := d.call(ctx, s, "Emulation.setDeviceMetricsOverride", map[string]any{"width": w, "height": h, "deviceScaleFactor": 1, "mobile": false}); err != nil {
		return err
	}
	time.Sleep(wait)
	for i := 0; i < scroll; i++ {
		// Wheel over the graph (left part of the page).
		if _, err := d.call(ctx, s, "Input.dispatchMouseEvent", map[string]any{"type": "mouseWheel", "x": 300, "y": 300, "deltaX": 0, "deltaY": 120}); err != nil {
			return err
		}
		time.Sleep(150 * time.Millisecond)
	}
	if scroll > 0 {
		time.Sleep(500 * time.Millisecond)
	}
	raw, err = d.call(ctx, s, "Page.captureScreenshot", map[string]any{"format": "png"})
	if err != nil {
		return err
	}
	var shot struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(raw, &shot); err != nil {
		return err
	}
	png, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	return os.WriteFile(out, png, 0o644)
}

func main() {
	chrome := flag.String("chrome", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "the Chrome binary")
	url := flag.String("url", "http://127.0.0.1:8765/", "the page")
	out := flag.String("out", "", "the PNG to write (outside any git work tree)")
	wait := flag.Duration("wait", 6*time.Second, "how long the page gets before the capture")
	scroll := flag.Int("scroll", 0, "wheel steps over the graph before the capture")
	w := flag.Int("w", 1680, "window width")
	h := flag.Int("h", 960, "window height")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "capture: -out is required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := run(ctx, *chrome, *url, *out, *wait, *scroll, *w, *h); err != nil {
		fmt.Fprintln(os.Stderr, "capture:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", *out)
}
