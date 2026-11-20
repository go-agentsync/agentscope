package ui

import (
	"math"

	"github.com/go-agentsync/agentscope/model"
)

// MaxBackingPixels bounds the canvas's backing store, in device pixels: a
// full-surface canvas at a high devicePixelRatio is hundreds of megabytes of
// pixels that a browser's compositor keeps copies of (a 5K display at 2x is 30 M
// pixels, 120 MB per copy).
const MaxBackingPixels = 8_000_000

// CapDPR is the device pixel ratio to draw a cssW by cssH viewport at: dpr, or
// less when the backing store would exceed MaxBackingPixels. A ratio that is not
// a positive number, or a viewport with no area, gives 1.
func CapDPR(cssW, cssH int, dpr float64) float64 {
	if !(dpr > 0) || math.IsInf(dpr, 0) || cssW < 1 || cssH < 1 {
		return 1
	}
	if px := float64(cssW) * float64(cssH) * dpr * dpr; px > MaxBackingPixels {
		dpr = math.Sqrt(MaxBackingPixels / (float64(cssW) * float64(cssH)))
	}
	return dpr
}

// Feed puts snapshots on the screen as sparingly as the owner can see: nothing is
// applied while the tab is hidden (the latest one is kept and shown once, when it
// is visible again), and a snapshot that changes nothing the screen shows
// repaints nothing (the Scene is only dirty when something it draws changed).
type Feed struct {
	scene  *Scene
	hidden bool
	held   *model.Snapshot
}

// NewFeed feeds scene.
func NewFeed(scene *Scene) *Feed { return &Feed{scene: scene} }

// Offer is a snapshot from the server. It reports whether it was applied now.
func (f *Feed) Offer(s model.Snapshot) bool {
	if f.hidden {
		f.held = &s // only the newest is worth keeping
		return false
	}
	f.scene.VM().Apply(s)
	return true
}

// SetHidden is the page's visibility changing. Becoming visible applies the
// snapshot held while it was hidden, and reports whether there was one: the host
// repaints once, whatever the answer.
func (f *Feed) SetHidden(hidden bool) (applied bool) {
	f.hidden = hidden
	if hidden || f.held == nil {
		return false
	}
	s := *f.held
	f.held = nil
	f.scene.VM().Apply(s)
	return true
}

// Hidden is whether the feed is paused.
func (f *Feed) Hidden() bool { return f.hidden }
