package serve

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-agentsync/agentscope/wire"
)

func newBound(t *testing.T) *Server {
	t.Helper()
	hub := wire.NewHub(&fakeSource{}, 50*time.Millisecond, 10*time.Second)
	srv, err := New(Config{Addr: "127.0.0.1:0", Web: fstest.MapFS{"index.html": {Data: []byte("x")}}, Scope: hub})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func serveWithin(t *testing.T, srv *Server) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- srv.Serve(context.Background()) }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
		return nil
	}
}

// A server whose HTTP half was shut down from outside stops cleanly: that is
// ErrServerClosed, which is a stop, not a failure.
func TestServeEndsCleanlyWhenTheHTTPServerWasShutDown(t *testing.T) {
	srv := newBound(t)
	if err := srv.http.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := serveWithin(t, srv); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

// A listener that fails is reported to the caller, and the server is shut down.
func TestServeReportsAListenerThatFails(t *testing.T) {
	srv := newBound(t)
	srv.ln.Close()
	err := serveWithin(t, srv)
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("err = %v, want the listener's own error", err)
	}
}
