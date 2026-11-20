package ui

import (
	"math"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
)

// An idle network: the same snapshot again and again, only its clock and the
// per-pass counters moving. Nothing the screen shows changes, so nothing is
// repainted: zero redraws, with the panel open on the anomalies, the Quota data
// in, and a filter on.
func TestAnIdleNetworkRepaintsNothing(t *testing.T) {
	s := newScene(t)
	vm := s.VM()
	vm.PanelOpen.Set(true)
	vm.Filters.State.Set(StateActive)
	first := withUsage(ctxSnap())
	vm.Apply(first)
	pixels(s)
	s.Dirty() // painted
	redraws := 0
	for i := 1; i <= 29; i++ { // snapshots 2 s apart, under a minute: no "Last" column ticks over
		next := withUsage(ctxSnap()) // a new Usage value, equal content
		next.At = first.At.Add(time.Duration(i) * 2 * time.Second)
		next.Usage.At += int64(2 * i) // the instant the usage view was built moves every pass
		next.Stats.ElapsedMicros = int64(10000 + i)
		next.Stats.BytesRead = int64(4096 * i)
		next.Stats.TranscriptsRead = i
		vm.Apply(next)
		if s.Dirty() {
			redraws++
		}
	}
	if redraws != 0 {
		t.Fatalf("%d redraws in 29 idle snapshots, want 0", redraws)
	}
	// A real change still repaints.
	changed := withUsage(ctxSnap())
	changed.Sessions[0].Context = 10
	vm.Apply(changed)
	if !s.Dirty() {
		t.Fatal("a change of content was not repainted")
	}
	// So does a change of the Quota data.
	u := withUsage(ctxSnap())
	u.Usage.QuotaErr = "boom"
	vm.Apply(u)
	if !s.Dirty() {
		t.Fatal("a change of the usage was not repainted")
	}
	// And a snapshot without usage after one with it.
	vm.Apply(ctxSnap())
	if vm.Usage.Get() != nil {
		t.Fatal("the usage did not go")
	}
}

func TestCapDPRKeepsTheBackingStoreUnderTheBound(t *testing.T) {
	for _, c := range []struct {
		w, h int
		dpr  float64
	}{{1280, 800, 2}, {2560, 1440, 2}, {5120, 2880, 2}, {5120, 2880, 3}, {1920, 1080, 1}, {10000, 10000, 1}} {
		got := CapDPR(c.w, c.h, c.dpr)
		px := float64(c.w) * got * float64(c.h) * got
		if px > MaxBackingPixels*1.0001 || got > c.dpr {
			t.Errorf("%dx%d at %v -> %v: %.0f pixels", c.w, c.h, c.dpr, got, px)
		}
	}
	if CapDPR(1280, 800, 2) != 2 { // 4.1 M pixels: under the bound, untouched
		t.Fatal("a small canvas was reduced")
	}
	if got := CapDPR(5120, 2880, 2); got >= 2 || got < 0.7 {
		t.Fatalf("%v", got)
	}
	for _, bad := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if CapDPR(100, 100, bad) != 1 {
			t.Fatalf("dpr %v", bad)
		}
	}
	if CapDPR(0, 100, 2) != 1 || CapDPR(100, -3, 2) != 1 {
		t.Fatal("no area")
	}
}

// While the tab is hidden nothing is applied (and so nothing is drawn); the
// newest snapshot is kept and shown once when it is visible again.
func TestFeedPausesWhileHiddenAndResumesWithOneApply(t *testing.T) {
	s := newScene(t)
	f := NewFeed(s)
	if f.Hidden() || !f.Offer(ctxSnap()) {
		t.Fatal("visible: applied at once")
	}
	s.Dirty()
	if f.SetHidden(true) || !f.Hidden() {
		t.Fatal("hiding applies nothing")
	}
	a, b := ctxSnap(), ctxSnap()
	a.Sessions[0].Context = 11
	b.Sessions[0].Context = 22
	if f.Offer(a) || f.Offer(b) {
		t.Fatal("applied while hidden")
	}
	if s.Dirty() {
		t.Fatal("a hidden tab was marked for repaint")
	}
	if !f.SetHidden(false) {
		t.Fatal("visible again applies the held snapshot")
	}
	if got, _ := s.VM().Snapshot(); got.Sessions[0].Context != 22 || !s.Dirty() {
		t.Fatal("only the newest held snapshot is shown, once")
	}
	if f.SetHidden(false) || f.SetHidden(true) {
		t.Fatal("nothing held, nothing to apply")
	}
	f.SetHidden(false)
	_ = model.Snapshot{}
}

// A Usage that cannot be hashed (a NaN is no JSON) is still a value to show, and
// never equal to a hash.
func TestUsageSigOfAnUnmarshalableUsage(t *testing.T) {
	u := &model.Usage{Capacity: model.Capacity{AccountWeeks: math.NaN()}}
	if usageSig(u) != "unhashable" || usageSig(nil) != "" || usageSig(&model.Usage{}) == "" {
		t.Fatal("usageSig")
	}
}

// The charts of the Quota tab are not rebuilt, and nothing is repainted, while
// another tab is shown; opening the tab builds them from the latest usage.
func TestQuotaChartsAreOnlyBuiltWhenTheTabIsShown(t *testing.T) {
	s := newScene(t)
	vm := s.VM()
	vm.Apply(withUsage(ctxSnap()))
	s.Dirty()
	u := withUsage(ctxSnap())
	u.Usage.QuotaErr = "later"
	vm.Apply(u)
	if s.Dirty() {
		t.Fatal("a usage change repainted the Graph tab")
	}
	vm.Tab.Set(TabQuota)
	if !s.Dirty() {
		t.Fatal("opening the tab did not repaint")
	}
	if s.quotaStale || vm.QuotaNotes.Get() == "" {
		t.Fatalf("the charts were not built on opening: %q", vm.QuotaNotes.Get())
	}
}
