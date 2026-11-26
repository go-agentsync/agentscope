package wire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
	wstransport "github.com/grpc-transports/websocket"
	"google.golang.org/grpc"
)

// fixed is a Scope that streams the snapshots it is given.
type fixed struct{ snaps []model.Snapshot }

func (f fixed) Snapshot(context.Context) (model.Snapshot, error) { return f.snaps[0], nil }
func (f fixed) Rename(context.Context, string, string) error     { return nil }
func (f fixed) SetColor(context.Context, string, string) error   { return nil }
func (f fixed) SetPrefs(context.Context, string, string) error   { return nil }
func (f fixed) Watch(ctx context.Context, send func(model.Snapshot) error) error {
	for _, s := range f.snaps {
		if err := send(s); err != nil {
			return err
		}
	}
	<-ctx.Done()
	return nil
}

func serveFixed(t *testing.T, sc Scope) string {
	t.Helper()
	wsh, wsl := wstransport.HandlerListener(wstransport.ServerConfig{Path: "/grpc"})
	gs := grpc.NewServer(ServerOptions()...)
	Register(gs, sc)
	mux := http.NewServeMux()
	mux.Handle("/grpc", wsh)
	ts := httptest.NewServer(mux)
	go gs.Serve(wsl)
	t.Cleanup(func() { gs.Stop(); ts.Close() })
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/grpc"
}

func TestWatchDeliversTheCurrentVersionAndRefusesANewerOne(t *testing.T) {
	url := serveFixed(t, fixed{snaps: []model.Snapshot{
		{Version: model.Version}, // the newest this client knows: delivered
		{Version: model.Version + 1},
		{Version: model.Version}, // never reached
	}})
	c, err := Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got := 0
	err = c.Watch(ctx, func(model.Snapshot) { got++ })
	if err == nil || !strings.Contains(err.Error(), "newer snapshot version") {
		t.Fatalf("err = %v, want the newer-version refusal", err)
	}
	if got != 1 {
		t.Fatalf("delivered %d snapshots, want only the one this client understands", got)
	}
}

func TestDialRefusesAURLAndATargetItCannotUse(t *testing.T) {
	if c, err := Dial(""); err == nil {
		c.Close()
		t.Error("an empty URL was accepted")
	}
	if c, err := dial("ws://127.0.0.1:1/grpc", grpc.WithDefaultServiceConfig("not json")); err == nil {
		c.Close()
		t.Error("a malformed gRPC option was accepted")
	}
}
