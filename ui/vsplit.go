package ui

import (
	"time"

	"github.com/go-widgets/mvvm"
	"github.com/go-widgets/toolkit"
)

// The split between the Sessions table and the Details pane inside the side panel:
// the owner drags the divider between them. Split is the Sessions pane's share of
// the two, in thousandths; each pane keeps at least PaneMinH logical pixels.
const (
	SplitScale   = 1000 // the two panes' shares add up to this
	SplitDefault = 550  // Sessions 55 %, Details 45 %: what a double-click on the divider restores
	SplitLo      = 100  // a stored or set share is held between these
	SplitHi      = 900
	PaneMinH     = 120 // the least height, in logical pixels, either pane keeps
	splitSnap    = 2   // a drag that ends this close to the default lands on it

	// minTableFloor is the height, in logical pixels, the Sessions table must keep
	// beside the filter rows; under it the rows leave the pane.
	minTableFloor = 36
)

// ClampSplit is a Sessions share held between SplitLo and SplitHi. The panes'
// minimum heights are applied by the layout, against the height the panel has.
func ClampSplit(v int) int { return min(max(v, SplitLo), SplitHi) }

// snapSplit lands a share within splitSnap thousandths of the default on it: the
// pixels of a pane never divide the panel exactly, and a reset must read as the
// default.
func snapSplit(v int) int {
	if v >= SplitDefault-splitSnap && v <= SplitDefault+splitSnap {
		return SplitDefault
	}
	return v
}

// splitShare is the Sessions share, in thousandths, of two panes laid out at a and
// b device pixels; the default when they have no height.
func splitShare(a, b int) int {
	if a+b <= 0 {
		return SplitDefault
	}
	return snapSplit(ClampSplit((a*SplitScale + (a+b)/2) / (a + b)))
}

// stack puts the Sessions and the Details cards one above the other in a vertical
// splitter. Its panes are given their own weights, mirrored from the ViewModel's
// Split (the mirrors of the splitter a layout replaces are dropped); what the owner
// does with the divider (a drag, a key, a double-click, which restores the
// default) is written back to Split, and a window resize is not.
func (s *Scene) stack(top, bottom toolkit.Widget) *toolkit.Splitter {
	for _, u := range s.splitUnsub {
		u()
	}
	vm := s.vm
	a, b := mvvm.NewObservable(vm.Split.Get()), mvvm.NewObservable(SplitScale-vm.Split.Get())
	s.splitUnsub = []func(){vm.Split.SubscribeChanged(func() {
		a.Set(vm.Split.Get())
		b.Set(SplitScale - vm.Split.Get())
	})}
	sp := toolkit.NewSplitter(true,
		&toolkit.SplitPane{Widget: top, Name: "Sessions", Weight: a, Min: PaneMinH, Default: SplitDefault},
		&toolkit.SplitPane{Widget: bottom, Name: "Details", Weight: b, Min: PaneMinH, Default: SplitScale - SplitDefault})
	sp.Now = func() time.Time { return s.clock() } // its double-click detection reads the scene's clock
	sp.OnResized = func([]int) { vm.Split.Set(splitShare(sp.PaneSize(0), sp.PaneSize(1))) }
	s.vsplit = sp
	return sp
}

// grabDivider gives a press at (x, y) to sp when it is on its divider.
func (s *Scene) grabDivider(sp *toolkit.Splitter, x, y int) bool {
	hr, ok := sp.HandleRect(0)
	if !ok || !inside(x, y, hr) {
		return false
	}
	sb := sp.Bounds()
	s.dragTarget, s.dragBounds, s.keyTarget = sp, sb, sp
	s.search.SetFocused(false)
	sp.OnEvent(local(toolkit.Event{Kind: toolkit.EventClick, X: x, Y: y}, sb))
	return true
}
