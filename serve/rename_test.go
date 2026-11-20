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

type fakeRenamer struct {
	mu   sync.Mutex
	got  [][2]string
	fail error
}

func (f *fakeRenamer) Rename(key, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, [2]string{key, name})
	return f.fail
}

// The rename the UI makes goes over the same WebSocket as the stream, and a
// server with no renamer, or whose renamer refuses, says so.
func TestRenameOverGRPC(t *testing.T) {
	srv, hub, _ := start(t)
	c, err := wire.Dial("ws://" + strings.TrimSuffix(strings.TrimPrefix(srv.URL(), "http://"), "/") + GRPCPath)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Rename(ctx, "7567abcd", "Labo (team)"); err == nil {
		t.Fatal("a server with no renamer accepted a rename")
	}
	r := &fakeRenamer{}
	hub.SetRenamer(r)
	if err := c.Rename(ctx, "7567abcd", "Labo (team)"); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	if len(r.got) != 1 || r.got[0] != [2]string{"7567abcd", "Labo (team)"} {
		t.Fatalf("%v", r.got)
	}
	r.mu.Unlock()
	r.fail = errors.New("not a usable display name")
	if err := c.Rename(ctx, "7567abcd", "x"); err == nil || !strings.Contains(err.Error(), "not a usable") {
		t.Fatalf("%v", err)
	}
}
