package ui

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/go-widgets/toolkit"
)

// ticking gives the scene a clock that moves a second per reading: two presses on
// the divider are then two drags, not a double-click.
func ticking(s *Scene) {
	var tick time.Duration
	s.clock = func() time.Time { tick += time.Second; return t0.Add(tick) }
}

// vopen opens the panel and returns the centre of the Sessions/Details divider.
func vopen(t *testing.T, s *Scene) (x, y int) {
	t.Helper()
	s.VM().PanelOpen.Set(true)
	pixels(s)
	hr, ok := s.vsplit.HandleRect(0)
	if !ok {
		t.Fatal("the panel has no Sessions/Details divider")
	}
	return center(hr)
}

// vdrag drags the divider from its place by dy and settles the scene.
func vdrag(t *testing.T, s *Scene, dy int) {
	t.Helper()
	x, y := center(vhandle(t, s))
	s.HandleClick(x, y)
	s.HandleMove(x, y+dy/2)
	s.HandleMove(x, y+dy)
	s.HandleRelease(x, y+dy)
	pixels(s)
}

func vhandle(t *testing.T, s *Scene) toolkit.Rect {
	t.Helper()
	r, ok := s.vsplit.HandleRect(0)
	if !ok {
		t.Fatal("no divider")
	}
	return r
}

func TestClampSplitBothEnds(t *testing.T) {
	for _, c := range []struct{ in, want int }{
		{SplitDefault, SplitDefault}, {SplitLo, SplitLo}, {SplitLo - 1, SplitLo}, {-5, SplitLo}, {0, SplitLo},
		{SplitHi, SplitHi}, {SplitHi + 1, SplitHi}, {99999, SplitHi},
	} {
		if got := ClampSplit(c.in); got != c.want {
			t.Errorf("ClampSplit(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestSnapSplitLandsNearTheDefaultOnItAndOnlyThere(t *testing.T) {
	for _, c := range []struct{ in, want int }{
		{SplitDefault, SplitDefault}, {SplitDefault - splitSnap, SplitDefault}, {SplitDefault + splitSnap, SplitDefault},
		{SplitDefault - splitSnap - 1, SplitDefault - splitSnap - 1}, {SplitDefault + splitSnap + 1, SplitDefault + splitSnap + 1},
	} {
		if got := snapSplit(c.in); got != c.want {
			t.Errorf("snapSplit(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestSplitShareOfTwoPanes(t *testing.T) {
	for _, c := range []struct{ a, b, want int }{
		{0, 0, SplitDefault}, {-3, 2, SplitDefault}, {300, 300, 500}, {1, 3, 250}, {2, 1, 667},
		{0, 500, SplitLo}, {500, 0, SplitHi}, // never outside the held range
		{550, 450, SplitDefault}, {551, 449, SplitDefault}, // pixel rounding reads as the default
	} {
		if got := splitShare(c.a, c.b); got != c.want {
			t.Errorf("splitShare(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestTheSplitIsViewModelStateHeldToItsRange(t *testing.T) {
	vm := NewViewModel()
	if vm.Split.Get() != SplitDefault {
		t.Fatalf("starts at %d", vm.Split.Get())
	}
	vm.Split.Set(700)
	if vm.Split.Get() != 700 {
		t.Fatalf("%d", vm.Split.Get())
	}
	vm.Split.Set(5000)
	if vm.Split.Get() != SplitHi {
		t.Fatalf("above the range: %d", vm.Split.Get())
	}
	vm.Split.Set(3)
	if vm.Split.Get() != SplitLo {
		t.Fatalf("below the range: %d", vm.Split.Get())
	}
}

// The panel opens at 55 / 45, Sessions over Details, each at least the minimum.
func TestThePanelOpensWithSessionsOverDetailsAt55To45(t *testing.T) {
	s := newScene(t)
	vopen(t, s)
	a, b := s.vsplit.PaneSize(0), s.vsplit.PaneSize(1)
	if got := splitShare(a, b); got != SplitDefault {
		t.Fatalf("panes %d / %d: share %d", a, b, got)
	}
	sb, db := s.table.Bounds(), s.detail.Bounds()
	if sb.Y+sb.H > db.Y {
		t.Fatalf("the table %v is not above the details %v", sb, db)
	}
	if !s.filtersShown {
		t.Fatal("a tall panel shows its filter rows")
	}
}

// Dragging the divider moves the split, both ways, and stops each pane at its
// minimum height.
func TestDraggingTheDividerResizesSessionsAndDetailsAndStopsAtTheMinimum(t *testing.T) {
	s := newScene(t)
	ticking(s)
	vm := s.VM()
	vopen(t, s)
	a0 := s.vsplit.PaneSize(0)
	vdrag(t, s, 60)
	if got := s.vsplit.PaneSize(0); got != a0+60 {
		t.Fatalf("dragged 60 down: Sessions %d -> %d", a0, got)
	}
	want := splitShare(s.vsplit.PaneSize(0), s.vsplit.PaneSize(1))
	if vm.Split.Get() != want || want <= SplitDefault {
		t.Fatalf("split %d, panes say %d", vm.Split.Get(), want)
	}
	vdrag(t, s, -100)
	if got := s.vsplit.PaneSize(0); got != a0-40 {
		t.Fatalf("dragged 100 up: Sessions %d", got)
	}
	// Far up stops at the minimum of Sessions, far down at the minimum of Details.
	vdrag(t, s, -9000)
	if a, b := s.vsplit.PaneSize(0), s.vsplit.PaneSize(1); a != PaneMinH || b < PaneMinH {
		t.Fatalf("far up: Sessions %d, Details %d", a, b)
	}
	if vm.Split.Get() < SplitLo || vm.Split.Get() >= SplitDefault {
		t.Fatalf("split %d", vm.Split.Get())
	}
	vdrag(t, s, 9000)
	if a, b := s.vsplit.PaneSize(0), s.vsplit.PaneSize(1); b != PaneMinH || a < PaneMinH {
		t.Fatalf("far down: Sessions %d, Details %d", a, b)
	}
	if vm.Split.Get() > SplitHi || vm.Split.Get() <= SplitDefault {
		t.Fatalf("split %d", vm.Split.Get())
	}
}

// The divider is a vertical resize and lights up under the pointer, and a press
// on it leaves the keyboard with it, so Escape closes the panel.
func TestTheDividerBetweenSessionsAndDetailsHasItsCursorHoverAndFocus(t *testing.T) {
	s := newScene(t)
	x, y := vopen(t, s)
	if got := s.Cursor(x, y); got != "row-resize" {
		t.Fatalf("over the divider: %q", got)
	}
	tx, ty := center(s.table.Bounds())
	if got := s.Cursor(tx, ty); got != "" {
		t.Fatalf("over the table: %q", got)
	}
	if !s.HandleMove(x, y) || s.HandleMove(x, y) {
		t.Fatal("hover on the divider changes the picture once")
	}
	if !s.HandleMove(tx, ty) || s.HandleMove(tx, ty) {
		t.Fatal("hover off the divider")
	}
	s.HandleClick(x, y)
	if got := s.Cursor(x, y+300); got != "row-resize" {
		t.Fatalf("held: %q", got)
	}
	s.HandleRelease(x, y)
	if s.keyTarget != toolkit.Widget(s.vsplit) || !s.inPanel(s.keyTarget) {
		t.Fatal("the divider is part of the panel")
	}
	// Closing the panel drops the hover.
	s.HandleMove(x, y)
	s.VM().PanelOpen.Set(false)
	if !s.HandleMove(x, y) || s.HandleMove(x, y) {
		t.Fatal("closing the panel leaves the hover")
	}
	if s.Cursor(x, y) != "" {
		t.Fatal("no cursor without the panel")
	}
}

// A double-click on the divider restores 55 / 45, and a lone press does not.
func TestADoubleClickOnTheDividerRestoresTheDefaultSplit(t *testing.T) {
	s := newScene(t)
	ticking(s)
	vm := s.VM()
	vopen(t, s)
	vdrag(t, s, 90)
	if vm.Split.Get() == SplitDefault {
		t.Fatal("the drag did not move the split")
	}
	// A press and a release alone leave it.
	x, y := center(vhandle(t, s))
	moved := vm.Split.Get()
	s.HandleClick(x, y)
	s.HandleRelease(x, y)
	if vm.Split.Get() != moved {
		t.Fatalf("a lone press changed the split: %d", vm.Split.Get())
	}
	// Two presses close together reset it: the scene's clock does not move.
	s.clock = func() time.Time { return t0 }
	s.HandleClick(x, y)
	s.HandleRelease(x, y)
	s.HandleClick(x, y)
	s.HandleRelease(x, y)
	pixels(s)
	if vm.Split.Get() != SplitDefault {
		t.Fatalf("after a double-click: %d", vm.Split.Get())
	}
	if got := splitShare(s.vsplit.PaneSize(0), s.vsplit.PaneSize(1)); got != SplitDefault {
		t.Fatalf("the panes show %d", got)
	}
}

// A window resize never rewrites the split, and neither pane goes under the
// minimum while the panel has the room for both.
func TestAWindowResizeKeepsTheSplitAndTheMinimums(t *testing.T) {
	defer toolkit.SetMetricScale(toolkit.MetricScale())
	st := &fakeStore{m: map[string]string{}}
	s := newScene(t)
	s.SetStore(st)
	vm := s.VM()
	for _, sp := range []int{SplitLo, SplitDefault, SplitHi} {
		vm.Split.Set(sp)
		for _, h := range []int{900, 700, 560, 520} {
			s.Resize(testW, h, 1)
			vm.PanelOpen.Set(true)
			pixels(s)
			a, b := s.vsplit.PaneSize(0), s.vsplit.PaneSize(1)
			if a < PaneMinH || b < PaneMinH {
				t.Fatalf("split %d, height %d: panes %d / %d under the minimum", sp, h, a, b)
			}
			if vm.Split.Get() != sp {
				t.Fatalf("a resize to %d rewrote the split: %d, was %d", h, vm.Split.Get(), sp)
			}
		}
	}
	if st.m[keySplit] != strconv.Itoa(SplitHi) {
		t.Fatalf("stored %q", st.m[keySplit])
	}
}

// Sessions at its minimum: the filter rows leave (and take no click), the table
// keeps the pane. Back at room, they return.
func TestAPaneAtItsMinimumHasNoRoomForTheFilterRows(t *testing.T) {
	s := newScene(t)
	vm := s.VM()
	vm.PanelOpen.Set(true)
	vm.Split.Set(SplitLo)
	pixels(s)
	if s.vsplit.PaneSize(0) != PaneMinH || s.filtersShown {
		t.Fatalf("Sessions %d, filters shown %v", s.vsplit.PaneSize(0), s.filtersShown)
	}
	for _, w := range s.targets() {
		if w == toolkit.Widget(s.stateDD) || w == toolkit.Widget(s.clearBtn) {
			t.Fatal("a filter row that is not laid out takes a click")
		}
	}
	if s.table.Bounds().H <= 0 {
		t.Fatal("the table has no room")
	}
	vm.Split.Set(SplitDefault)
	pixels(s)
	if !s.filtersShown {
		t.Fatal("the rows did not come back")
	}
	found := false
	for _, w := range s.targets() {
		found = found || w == toolkit.Widget(s.stateDD)
	}
	if !found {
		t.Fatal("the shown filter rows take no click")
	}
}

// What the viewer stored comes back; a corrupt or missing value is the default
// (or held to its range), and the value written is the one that holds.
func TestTheSplitIsPersistedPerViewerAndRestoredSafely(t *testing.T) {
	st := &fakeStore{m: map[string]string{}}
	s := newScene(t)
	ticking(s)
	s.SetStore(st)
	if len(st.sets) != 0 {
		t.Fatal("nothing is written before the owner changes something")
	}
	vopen(t, s)
	vdrag(t, s, 70)
	got := st.m[keySplit]
	if n, err := strconv.Atoi(got); err != nil || n != s.VM().Split.Get() || n <= SplitDefault {
		t.Fatalf("stored %q, split %d", got, s.VM().Split.Get())
	}
	s2 := NewScene(testW, testH)
	s2.SetStore(st)
	if s2.VM().Split.Get() != s.VM().Split.Get() {
		t.Fatalf("restored %d, want %d", s2.VM().Split.Get(), s.VM().Split.Get())
	}
	for _, c := range []struct {
		name  string
		store map[string]string
		want  int
	}{
		{"missing", map[string]string{}, SplitDefault},
		{"empty", map[string]string{keySplit: ""}, SplitDefault},
		{"garbage", map[string]string{keySplit: "tall"}, SplitDefault},
		{"fraction", map[string]string{keySplit: "0.5"}, SplitDefault},
		{"padded", map[string]string{keySplit: " 640 "}, 640},
		{"too big", map[string]string{keySplit: "99999"}, SplitHi},
		{"negative", map[string]string{keySplit: "-5"}, SplitLo},
	} {
		sc := NewScene(testW, testH)
		sc.SetStore(&fakeStore{m: c.store})
		if got := sc.VM().Split.Get(); got != c.want {
			t.Errorf("%s: split %d, want %d", c.name, got, c.want)
		}
		sc.VM().PanelOpen.Set(true)
		pixels(sc)
		if a, b := sc.vsplit.PaneSize(0), sc.vsplit.PaneSize(1); a < PaneMinH || b < PaneMinH {
			t.Errorf("%s: panes %d / %d", c.name, a, b)
		}
	}
}

// Nothing leaves its frame with the panel open at the least height that holds both
// panes at their minimum, and below it, whatever the split.
func TestNothingLeavesItsFrameWithBothPanesAtTheirMinimum(t *testing.T) {
	defer toolkit.SetMetricScale(toolkit.MetricScale())
	for _, scale := range []float64{1, 2} {
		minH := 0
		for h := 300; h <= 900 && minH == 0; h += 4 {
			s := NewScene(testW, h)
			s.Resize(int(float64(testW)*scale), int(float64(h)*scale), scale)
			s.VM().PanelOpen.Set(true)
			pixels(s)
			px := int(PaneMinH*scale + 0.5)
			if s.vsplit.PaneSize(0) >= px && s.vsplit.PaneSize(1) >= px {
				minH = h
			}
		}
		if minH == 0 {
			t.Fatalf("scale %g: no window height holds both panes at their minimum", scale)
		}
		for _, h := range []int{minH - 40, minH, minH + 4, minH + 100} {
			for _, sp := range []int{SplitLo, SplitDefault, SplitHi} {
				name := fmt.Sprintf("height %d (minimum %d) at %gx, split %d", h, minH, scale, sp)
				s := NewScene(testW, h)
				s.Resize(int(float64(testW)*scale), int(float64(h)*scale), scale)
				vm := s.VM()
				vm.Apply(snapWith2(withUsage(crowded()), guardFixture()))
				vm.Split.Set(sp)
				vm.PanelOpen.Set(true)
				vm.Graph.Selected.Set("node:s:" + crowded().Sessions[0].Short)
				pixels(s)
				if got := outside(s); len(got) != 0 {
					t.Fatalf("%s: %v", name, got[0])
				}
				if h == minH {
					px := int(PaneMinH*scale + 0.5)
					if a, b := s.vsplit.PaneSize(0), s.vsplit.PaneSize(1); a < px || b < px {
						t.Fatalf("%s: panes %d / %d, minimum %d", name, a, b, px)
					}
					// The table and the details sit inside their own panes.
					top, bot := s.vsplit.PaneSize(0), s.vsplit.PaneSize(1)
					tb, db := s.table.Bounds(), s.detail.Bounds()
					hr := vhandle(t, s)
					if tb.Y+tb.H > hr.Y || db.Y < hr.Y+hr.H || top <= 0 || bot <= 0 {
						t.Fatalf("%s: table %v, details %v, divider %v", name, tb, db, hr)
					}
				}
			}
		}
	}
}
