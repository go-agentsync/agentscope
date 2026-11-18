package serve

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-agentsync/agentscope/wire"
)

type fakeSource struct{ n atomic.Int64 }

func (f *fakeSource) Snapshot() model.Snapshot {
	return model.Snapshot{Version: model.Version, At: time.Now(), Sessions: []model.Session{{ID: "11111111-1111-1111-1111-111111111111", Short: "11111111", Title: "t" + string(rune('a'+f.n.Add(1)))}}}
}

func start(t *testing.T) (*Server, *wire.Hub, context.CancelFunc) {
	t.Helper()
	hub := wire.NewHub(&fakeSource{}, 50*time.Millisecond, 10*time.Second)
	web := fstest.MapFS{"index.html": {Data: []byte("<html>agentscope</html>")}}
	srv, err := New(Config{Addr: "127.0.0.1:0", Web: web, Scope: hub})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Serve did not stop")
		}
	})
	return srv, hub, cancel
}

func get(t *testing.T, srv *Server, host, method, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, strings.TrimSuffix(srv.URL(), "/")+path, nil)
	if host != "" {
		req.Host = host
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestServesTheUIWithHardeningHeaders(t *testing.T) {
	srv, _, _ := start(t)
	resp := get(t, srv, "", "GET", "/")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "agentscope") {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy", "Cache-Control"} {
		if resp.Header.Get(h) == "" {
			t.Errorf("missing header %s", h)
		}
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "unsafe-inline") {
		t.Errorf("csp %q", csp)
	}
}

func TestRebindingHostIsRefused(t *testing.T) {
	srv, _, _ := start(t)
	for _, host := range []string{"evil.example", "evil.example:80", "127.0.0.1", "127.0.0.1.evil.example:" + portOf(srv)} {
		if resp := get(t, srv, host, "GET", "/"); resp.StatusCode != http.StatusMisdirectedRequest {
			t.Errorf("Host %q: status %d, want 421", host, resp.StatusCode)
		}
		if resp := get(t, srv, host, "GET", GRPCPath); resp.StatusCode != http.StatusMisdirectedRequest {
			t.Errorf("Host %q on %s: status %d, want 421", host, GRPCPath, resp.StatusCode)
		}
	}
	// "localhost" with the right port is the loopback name and is accepted.
	if resp := get(t, srv, "localhost:"+portOf(srv), "GET", "/"); resp.StatusCode != 200 {
		t.Errorf("localhost: status %d", resp.StatusCode)
	}
}

func portOf(s *Server) string { return s.URL()[strings.LastIndex(s.URL(), ":")+1 : len(s.URL())-1] }

func TestOnlyReadMethods(t *testing.T) {
	srv, _, _ := start(t)
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		if resp := get(t, srv, "", m, "/"); resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: status %d, want 405", m, resp.StatusCode)
		}
	}
	if resp := get(t, srv, "", "HEAD", "/"); resp.StatusCode != 200 {
		t.Errorf("HEAD: %d", resp.StatusCode)
	}
}

func TestLoopbackOnly(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", "192.0.2.1:80", ":0", "example.com:80", "127.0.0.1:99999", "127.0.0.1:x", "nonsense"} {
		if _, err := New(Config{Addr: addr, Web: fstest.MapFS{}, Scope: wire.NewHub(&fakeSource{}, time.Second, time.Second)}); err == nil {
			t.Errorf("%q was accepted", addr)
		}
	}
	if _, err := New(Config{Addr: "[::1]:0", Web: fstest.MapFS{}, Scope: wire.NewHub(&fakeSource{}, time.Second, time.Second)}); err != nil {
		t.Logf("no IPv6 loopback here: %v", err)
	}
}

func TestPortInUse(t *testing.T) {
	srv, _, _ := start(t)
	if _, err := New(Config{Addr: "127.0.0.1:" + portOf(srv), Web: fstest.MapFS{}, Scope: wire.NewHub(&fakeSource{}, time.Second, time.Second)}); err == nil {
		t.Fatal("a second listener on the same port was accepted")
	}
}

// The browser path: the same client code the wasm UI runs, native, over the
// real WebSocket and the real gRPC server.
func TestGRPCOverWebSocket(t *testing.T) {
	srv, _, _ := start(t)
	c, err := wire.Dial("ws://" + strings.TrimSuffix(strings.TrimPrefix(srv.URL(), "http://"), "/") + GRPCPath)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	snap, err := c.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Sessions) != 1 || snap.Sessions[0].Short != "11111111" {
		t.Fatalf("snapshot %+v", snap)
	}
	var got []string
	wctx, wcancel := context.WithCancel(ctx)
	err = c.Watch(wctx, func(s model.Snapshot) {
		got = append(got, s.Sessions[0].Title)
		if len(got) == 3 {
			wcancel()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] == got[1] {
		t.Fatalf("a changing source must stream changes: %v", got)
	}
}

func TestWebSocketRefusesAForeignOrigin(t *testing.T) {
	srv, _, _ := start(t)
	req, _ := http.NewRequest("GET", strings.TrimSuffix(srv.URL(), "/")+GRPCPath, nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Origin", "http://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusSwitchingProtocols {
		t.Fatal("a foreign Origin opened the stream")
	}
}

var _ = httptest.NewRecorder
