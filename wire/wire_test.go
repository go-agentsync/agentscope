package wire

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"google.golang.org/grpc/mem"
)

func TestCodecRoundTripAndTrailingData(t *testing.T) {
	in := model.Snapshot{Version: 1, Sessions: []model.Session{{ID: "x"}}}
	b, err := Codec.Marshal(&in)
	if err != nil {
		t.Fatal(err)
	}
	var out model.Snapshot
	if err := Codec.Unmarshal(b, &out); err != nil || len(out.Sessions) != 1 {
		t.Fatalf("%v %+v", err, out)
	}
	if Codec.Name() != "json" {
		t.Error("name")
	}
	for _, bad := range []string{`{"version":1} {"version":2}`, `{"version":1}x`, `{`, ``} {
		if err := Codec.Unmarshal(mem.BufferSlice{mem.SliceBuffer([]byte(bad))}, &out); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := Codec.Marshal(func() {}); err == nil {
		t.Error("an unmarshalable value was accepted")
	}
}

func FuzzCodecUnmarshal(f *testing.F) {
	f.Add([]byte(`{"version":1,"sessions":[{"id":"x"}]}`))
	f.Add([]byte(`{"sessions":{}}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, b []byte) {
		var s model.Snapshot
		if err := Codec.Unmarshal(mem.BufferSlice{mem.SliceBuffer(b)}, &s); err == nil {
			// Whatever decoded must encode again.
			if _, err := json.Marshal(s); err != nil {
				t.Fatal(err)
			}
		}
	})
}

type src struct {
	n    atomic.Int64
	vary atomic.Bool
}

func (s *src) Snapshot() model.Snapshot {
	n := s.n.Add(1)
	snap := model.Snapshot{Version: 1, At: time.Unix(1_700_000_000+n*100, 0)}
	if s.vary.Load() {
		snap.Sessions = []model.Session{{ID: "v", Sent: int(n)}}
	}
	return snap
}

func TestHubPublishesChangesAndKeepalives(t *testing.T) {
	s := &src{}
	h := NewHub(s, time.Hour, 250*time.Second)
	h.Collect()
	h.Collect() // same content, 100 s later: no keepalive due yet
	if h.seq != 1 {
		t.Fatalf("an unchanged snapshot republished: seq %d", h.seq)
	}
	h.Collect() // 200 s
	h.Collect() // 300 s: keepalive due
	if h.seq != 2 {
		t.Fatalf("keepalive: seq %d", h.seq)
	}
	s.vary.Store(true)
	h.Collect()
	if h.seq != 3 {
		t.Fatalf("a change must publish: seq %d", h.seq)
	}
	// The clock alone and the per-pass read counters are not a change.
	a, b := model.Snapshot{At: time.Unix(1, 0)}, model.Snapshot{At: time.Unix(99, 0)}
	b.Stats.ElapsedMicros, b.Stats.BytesRead, b.Stats.TranscriptsRead = 5, 6, 7
	if string(fingerprint(a)) != string(fingerprint(b)) {
		t.Error("fingerprint must ignore At and the per-pass counters")
	}
}

func TestHubWatchAndSnapshot(t *testing.T) {
	s := &src{}
	h := NewHub(s, 10*time.Millisecond, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Snapshot before any collection waits for the first one.
	go func() { time.Sleep(20 * time.Millisecond); h.Collect() }()
	first, err := h.Snapshot(ctx)
	if err != nil || first.Version != 1 {
		t.Fatal(err)
	}
	if again, _ := h.Snapshot(ctx); !again.At.Equal(first.At) {
		t.Error("the latest snapshot is returned")
	}
	go h.Run(ctx)
	s.vary.Store(true)
	var got int
	wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
	defer wcancel()
	err = h.Watch(wctx, func(model.Snapshot) error {
		got++
		if got == 2 {
			wcancel()
		}
		return nil
	})
	if err != nil || got != 2 {
		t.Errorf("watch: %v, %d snapshots", err, got)
	}
	// A failing send ends the watch with its error.
	boom := errors.New("send failed")
	if err := h.Watch(ctx, func(model.Snapshot) error { return boom }); !errors.Is(err, boom) {
		t.Errorf("%v", err)
	}
	// Snapshot honours a cancelled context while waiting.
	empty := NewHub(s, time.Hour, time.Hour)
	cctx, ccancel := context.WithCancel(ctx)
	ccancel()
	if _, err := empty.Snapshot(cctx); err == nil {
		t.Error("a cancelled context must stop the wait")
	}
}

func TestDialErrors(t *testing.T) {
	if bad, err := Dial("http://not-a-websocket-url"); err == nil {
		// The connection is lazy: the first call is where a bad URL fails.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := bad.Snapshot(ctx); err == nil {
			t.Error("a non-ws URL was served")
		}
		bad.Close()
	}
	c, err := Dial("ws://127.0.0.1:1/grpc")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.Snapshot(ctx); err == nil {
		t.Error("nothing listens there")
	}
	if err := c.Watch(ctx, func(model.Snapshot) {}); err == nil {
		t.Error("nothing listens there")
	}
}
