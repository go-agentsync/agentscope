package wire

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/go-agentsync/agentscope/model"
)

// Source produces snapshots; *collect.Collector is one.
type Source interface{ Snapshot() model.Snapshot }

// Hub polls a Source and fans the snapshots out to every Watch stream. It
// collects once per interval however many browsers are watching, and sends a
// stream a snapshot only when something besides the clock changed, plus a
// keepalive so a quiet network still shows a moving "updated" time.
type Hub struct {
	src       Source
	every     time.Duration
	keepalive time.Duration

	mu      sync.Mutex
	latest  model.Snapshot
	have    bool
	seq     uint64
	changed chan struct{} // closed and replaced on every publish
	sum     []byte
	sentAt  time.Time
}

// NewHub returns a Hub that collects every `every` and republishes an
// unchanged snapshot after `keepalive`.
func NewHub(src Source, every, keepalive time.Duration) *Hub {
	return &Hub{src: src, every: every, keepalive: keepalive, changed: make(chan struct{})}
}

// Run collects until ctx ends. The first snapshot is taken before it returns
// from the first iteration, so a client that connects at once is served.
func (h *Hub) Run(ctx context.Context) {
	h.Collect()
	t := time.NewTicker(h.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.Collect()
		}
	}
}

// fingerprint is the snapshot without what changes on every pass.
func fingerprint(s model.Snapshot) []byte {
	s.At = time.Time{}
	s.Stats.ElapsedMicros, s.Stats.BytesRead, s.Stats.TranscriptsRead = 0, 0, 0
	b, _ := json.Marshal(s)
	return b
}

// Collect takes one snapshot and publishes it when it differs, or when the
// keepalive is due.
func (h *Hub) Collect() {
	s := h.src.Snapshot()
	sum := fingerprint(s)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.have && bytes.Equal(sum, h.sum) && s.At.Sub(h.sentAt) < h.keepalive {
		return
	}
	h.latest, h.have, h.sum, h.sentAt = s, true, sum, s.At
	h.seq++
	close(h.changed)
	h.changed = make(chan struct{})
}

// wait returns the latest snapshot if its sequence is past seen, else blocks
// until one is published.
func (h *Hub) wait(ctx context.Context, seen uint64) (model.Snapshot, uint64, error) {
	for {
		h.mu.Lock()
		if h.have && h.seq > seen {
			s, q := h.latest, h.seq
			h.mu.Unlock()
			return s, q, nil
		}
		ch := h.changed
		h.mu.Unlock()
		select {
		case <-ctx.Done():
			return model.Snapshot{}, 0, ctx.Err()
		case <-ch:
		}
	}
}

// Snapshot implements Scope: the latest, or a fresh one before the first.
func (h *Hub) Snapshot(ctx context.Context) (model.Snapshot, error) {
	h.mu.Lock()
	s, ok := h.latest, h.have
	h.mu.Unlock()
	if ok {
		return s, nil
	}
	s, _, err := h.wait(ctx, 0)
	return s, err
}

// Watch implements Scope.
func (h *Hub) Watch(ctx context.Context, send func(model.Snapshot) error) error {
	var seen uint64
	for {
		s, q, err := h.wait(ctx, seen)
		if err != nil {
			return nil // the client went away
		}
		seen = q
		if err := send(s); err != nil {
			return err
		}
	}
}
