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

type fakeColorer struct {
	mu   sync.Mutex
	got  [][2]string
	fail error
}

func (f *fakeColorer) SetColor(key, color string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, [2]string{key, color})
	return f.fail
}

// The colour the UI picks goes over the same WebSocket as the stream, and a
// server with no colourer, or whose colourer refuses, says so.
func TestSetColorOverGRPC(t *testing.T) {
	srv, hub, _ := start(t)
	c, err := wire.Dial("ws://" + strings.TrimSuffix(strings.TrimPrefix(srv.URL(), "http://"), "/") + GRPCPath)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.SetColor(ctx, "7567abcd", "#e69f00"); err == nil {
		t.Fatal("a server with no colourer accepted a colour")
	}
	f := &fakeColorer{}
	hub.SetColorer(f)
	if err := c.SetColor(ctx, "7567abcd", "#e69f00"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if len(f.got) != 1 || f.got[0] != [2]string{"7567abcd", "#e69f00"} {
		t.Fatalf("%v", f.got)
	}
	f.mu.Unlock()
	f.fail = errors.New("a colour is #rrggbb")
	if err := c.SetColor(ctx, "7567abcd", "red"); err == nil || !strings.Contains(err.Error(), "#rrggbb") {
		t.Fatalf("%v", err)
	}
}
