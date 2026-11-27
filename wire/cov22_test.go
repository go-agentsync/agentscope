package wire

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
)

type recorder struct {
	key, val, val2 string
	err            error
}

func (r *recorder) Rename(key, name string) error     { r.key, r.val = key, name; return r.err }
func (r *recorder) SetColor(key, color string) error  { r.key, r.val = key, color; return r.err }
func (r *recorder) SetPrefs(theme, lang string) error { r.val, r.val2 = theme, lang; return r.err }
func (r *recorder) set() (string, string, string)     { return r.key, r.val, r.val2 }

// The client's three write calls reach the server's Scope with their arguments.
func TestClientWritesReachTheScope(t *testing.T) {
	h := NewHub(&src{}, time.Hour, time.Hour)
	rec := &recorder{}
	h.SetRenamer(rec)
	h.SetColorer(rec)
	h.SetPrefser(rec)
	c, err := Dial(serveFixed(t, h))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.Rename(ctx, "k1", "Work"); err != nil {
		t.Fatal(err)
	}
	if k, v, _ := rec.set(); k != "k1" || v != "Work" {
		t.Fatalf("Rename reached the scope as %q %q", k, v)
	}
	if err := c.SetColor(ctx, "k2", "#112233"); err != nil {
		t.Fatal(err)
	}
	if k, v, _ := rec.set(); k != "k2" || v != "#112233" {
		t.Fatalf("SetColor reached the scope as %q %q", k, v)
	}
	if err := c.SetPrefs(ctx, "dark", "fr"); err != nil {
		t.Fatal(err)
	}
	if _, v, v2 := rec.set(); v != "dark" || v2 != "fr" {
		t.Fatalf("SetPrefs reached the scope as %q %q", v, v2)
	}
}

// A hub with no Renamer refuses; with one, it writes then collects, and a
// failing Renamer's error is returned without collecting.
func TestHubRenameRefusesDelegatesAndReportsFailure(t *testing.T) {
	h := NewHub(&src{}, time.Hour, time.Hour)
	if err := h.Rename(context.Background(), "k", "n"); err == nil {
		t.Fatal("a hub with no Renamer renamed an account")
	}
	rec := &recorder{}
	h.SetRenamer(rec)
	if err := h.Rename(context.Background(), "k", "n"); err != nil {
		t.Fatal(err)
	}
	if k, v, _ := rec.set(); k != "k" || v != "n" {
		t.Fatalf("Rename wrote %q %q", k, v)
	}
	h.mu.Lock()
	seq := h.seq
	h.mu.Unlock()
	if seq == 0 {
		t.Fatal("Rename did not collect")
	}
	rec.err = errors.New("boom")
	if err := h.Rename(context.Background(), "k", "n"); !errors.Is(err, rec.err) {
		t.Fatalf("err = %v, want the Renamer's", err)
	}
}

// Same for SetColor: delegation and the failing Colorer.
func TestHubSetColorDelegatesAndReportsFailure(t *testing.T) {
	h := NewHub(&src{}, time.Hour, time.Hour)
	rec := &recorder{}
	h.SetColorer(rec)
	if err := h.SetColor(context.Background(), "k", "#010203"); err != nil {
		t.Fatal(err)
	}
	if k, v, _ := rec.set(); k != "k" || v != "#010203" {
		t.Fatalf("SetColor wrote %q %q", k, v)
	}
	rec.err = errors.New("boom")
	if err := h.SetColor(context.Background(), "k", "#010203"); !errors.Is(err, rec.err) {
		t.Fatalf("err = %v, want the Colorer's", err)
	}
}

// Run collects once at once, again on each tick, and returns when ctx ends.
func TestHubRunCollectsOnEachTickAndStops(t *testing.T) {
	h := NewHub(&src{}, time.Millisecond, 0)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.Run(ctx); close(done) }()
	deadline := time.After(10 * time.Second)
	for {
		h.mu.Lock()
		seq := h.seq
		h.mu.Unlock()
		if seq >= 3 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("Run published %d snapshots, want a tick to publish more", seq)
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after ctx ended")
	}
}

// A send that fails ends Watch with that error.
func TestHubWatchReturnsTheSendError(t *testing.T) {
	h := NewHub(&src{}, time.Hour, time.Hour)
	h.Collect()
	boom := errors.New("boom")
	if err := h.Watch(context.Background(), func(model.Snapshot) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the send error", err)
	}
}

// endsAtOnce is a Scope whose stream ends after one snapshot.
type endsAtOnce struct{ fixed }

func (e endsAtOnce) Watch(_ context.Context, send func(model.Snapshot) error) error {
	return send(model.Snapshot{Version: model.Version})
}

// Watch returns nil both when the server ends the stream and when ctx is
// cancelled by the caller.
func TestWatchReturnsNilWhenTheStreamEndsOrTheCallerCancels(t *testing.T) {
	c, err := Dial(serveFixed(t, endsAtOnce{fixed{snaps: []model.Snapshot{{Version: model.Version}}}}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	got := 0
	if err := c.Watch(context.Background(), func(model.Snapshot) { got++ }); err != nil || got != 1 {
		t.Fatalf("server-ended stream: err = %v after %d snapshots, want nil after 1", err, got)
	}

	c2, err := Dial(serveFixed(t, fixed{snaps: []model.Snapshot{{Version: model.Version}}}))
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	ctx, cancel := context.WithCancel(context.Background())
	if err := c2.Watch(ctx, func(model.Snapshot) { cancel() }); err != nil {
		t.Fatalf("cancelled stream: err = %v, want nil", err)
	}
}
