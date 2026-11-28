// Command capture takes a screenshot of a running agentscope page with a real
// Chrome, after the wasm UI has had time to receive its stream, and can scroll
// the graph first. Headless Chrome's own --screenshot fires at the load event,
// before the wasm has booted, so it shoots an empty window.
//
//	go run ./internal/capture --chrome "<path>" --url http://127.0.0.1:8765/ \
//	    --out ~/Library/Application\ Support/agentscope/captures/live.png --wait 6s --scroll 0
//
// A capture shows real session ids and lease notes, so it is refused when --out
// is inside a git work tree (any parent directory holds a .git).
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	"github.com/spf13/cobra"
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

// dragSpec is the -drag flag: "x1,y1,x2,y2", done after the clicks.
var dragSpec, evalExpr string

// portPoll is how often run looks for the DevTools port file, 100 times.
var portPoll = 100 * time.Millisecond

func run(ctx context.Context, chrome, url, out string, wait time.Duration, scroll, w, h int, click string, dark bool) error {
	if in, err := insideGitTree(out); err != nil || in {
		if err == nil {
			return fmt.Errorf("refusing to write a capture inside a git work tree (%s)", out)
		}
		return fmt.Errorf("refusing to write a capture: cannot tell whether %s is inside a git work tree: %w", out, err)
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
		time.Sleep(portPoll)
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
	// Always emulated, so a capture is the scheme asked for and not whatever the
	// machine running it prefers.
	scheme := "light"
	if dark {
		scheme = "dark"
	}
	if _, err := d.call(ctx, s, "Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": scheme}}}); err != nil {
		return err
	}
	time.Sleep(wait)
	if click != "" {
		// One click or several, "x,y;x,y", in order.
		for _, one := range strings.Split(click, ";") {
			var x, y int
			if _, err := fmt.Sscanf(one, "%d,%d", &x, &y); err != nil {
				return fmt.Errorf("-click wants x,y[;x,y...]: %v", err)
			}
			for _, typ := range []string{"mousePressed", "mouseReleased"} {
				if _, err := d.call(ctx, s, "Input.dispatchMouseEvent", map[string]any{"type": typ, "x": x, "y": y, "button": "left", "clickCount": 1}); err != nil {
					return err
				}
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	if evalExpr != "" { // a measurement: evaluate in the page and print the result
		raw, err := d.call(ctx, s, "Runtime.evaluate", map[string]any{"expression": evalExpr, "returnByValue": true})
		if err != nil {
			return err
		}
		fmt.Fprintln(evalOut, "eval:", string(raw))
	}
	if dragSpec != "" { // press at x1,y1, move to x2,y2, release: a drag of a divider
		var x1, y1, x2, y2 int
		if _, err := fmt.Sscanf(dragSpec, "%d,%d,%d,%d", &x1, &y1, &x2, &y2); err != nil {
			return fmt.Errorf("-drag wants x1,y1,x2,y2: %v", err)
		}
		for _, ev := range []map[string]any{
			{"type": "mouseMoved", "x": x1, "y": y1},
			{"type": "mousePressed", "x": x1, "y": y1, "button": "left", "clickCount": 1},
			{"type": "mouseMoved", "x": (x1 + x2) / 2, "y": y1, "button": "left"},
			{"type": "mouseMoved", "x": x2, "y": y2, "button": "left"},
			{"type": "mouseReleased", "x": x2, "y": y2, "button": "left", "clickCount": 1},
		} {
			if _, err := d.call(ctx, s, "Input.dispatchMouseEvent", ev); err != nil {
				return err
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
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

// osExit is replaced by the tests.
var osExit = os.Exit

// evalOut receives the result of --eval.
var evalOut io.Writer = os.Stdout

// RootCmd is the command line, driven by the tests through SetArgs, SetOut and
// SetErr.
func RootCmd() *cobra.Command {
	var chrome, url, out, click string
	var wait time.Duration
	var scroll, w, h int
	var dark bool
	cmd := &cobra.Command{
		Use:           "capture",
		Short:         "Screenshot a running agentscope page with a real Chrome",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), wait+90*time.Second)
			defer cancel()
			evalOut = cmd.OutOrStdout()
			if err := run(ctx, chrome, url, out, wait, scroll, w, h, click, dark); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "wrote", out)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&chrome, "chrome", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "the Chrome binary")
	f.StringVar(&url, "url", "http://127.0.0.1:8765/", "the page")
	f.StringVar(&out, "out", "", "the PNG to write (outside any git work tree)")
	f.DurationVar(&wait, "wait", 6*time.Second, "how long the page gets before the capture")
	f.IntVar(&scroll, "scroll", 0, "wheel steps over the graph before the capture")
	f.StringVar(&click, "click", "", "x,y of a click (e.g. a node) before the capture; x,y;x,y for several")
	f.StringVar(&dragSpec, "drag", "", "x1,y1,x2,y2: press, move and release (resizing the panel's divider) after the clicks")
	f.StringVar(&evalExpr, "eval", "", "a JavaScript expression evaluated in the page after the wait, its result printed (e.g. agentscopeStats())")
	f.BoolVar(&dark, "dark", false, "emulate prefers-color-scheme: dark")
	f.IntVar(&w, "width", 1680, "window width")
	f.IntVar(&h, "height", 960, "window height")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

func main() {
	cmd := RootCmd()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "capture:", err)
		osExit(1)
	}
}
