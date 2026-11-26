package serve

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/wire"
)

type fakePrefser struct {
	mu   sync.Mutex
	got  [][2]string
	fail error
}

func (f *fakePrefser) SetPrefs(theme, lang string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, [2]string{theme, lang})
	return f.fail
}

// The theme and language the UI picks go over the same WebSocket as the stream,
// and a server with no Prefser, or whose Prefser refuses, says so.
func TestSetPrefsOverGRPC(t *testing.T) {
	srv, hub, _ := start(t)
	c, err := wire.Dial("ws://" + strings.TrimSuffix(strings.TrimPrefix(srv.URL(), "http://"), "/") + GRPCPath)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.SetPrefs(ctx, "dark", "fr"); err == nil {
		t.Fatal("a server with no Prefser accepted a preference")
	}
	f := &fakePrefser{}
	hub.SetPrefser(f)
	if err := c.SetPrefs(ctx, "dark", "fr"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if len(f.got) != 1 || f.got[0] != [2]string{"dark", "fr"} {
		t.Fatalf("%v", f.got)
	}
	f.mu.Unlock()
	f.fail = errors.New("a theme is light, dark or system")
	if err := c.SetPrefs(ctx, "blue", ""); err == nil || !strings.Contains(err.Error(), "light, dark or system") {
		t.Fatalf("%v", err)
	}
}
