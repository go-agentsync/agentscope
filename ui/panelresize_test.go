package ui

import (
	"strconv"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/toolkit"
)

func TestClampPanelWBothEnds(t *testing.T) {
	for _, c := range []struct{ w, view, want int }{
		{420, 1600, 420}, {PanelMinW, 1600, PanelMinW}, {PanelMinW - 1, 1600, PanelMinW}, {0, 1600, PanelMinW}, {-9, 1600, PanelMinW},
		{1120, 1600, 1120}, {1121, 1600, 1120}, {9999, 1600, 1120}, // 70 % of 1600
		{9999, 800, 560}, {400, 800, 400},
		{9999, 400, PanelMinW}, // a surface too narrow for the minimum: the minimum
		{500, 0, PanelMinW},
	} {
		if got := ClampPanelW(c.w, c.view); got != c.want {
			t.Errorf("ClampPanelW(%d, %d) = %d, want %d", c.w, c.view, got, c.want)
		}
	}
}

func TestPanelWidthIsViewModelStateHeldToItsBounds(t *testing.T) {
	vm := NewViewModel()
	vm.ViewW.Set(1000)
	vm.PanelW.Set(500)
	if vm.PanelW.Get() != 500 {
		t.Fatal(vm.PanelW.Get())
	}
	vm.PanelW.Set(5000)
	if vm.PanelW.Get() != 700 {
		t.Fatalf("above 70 %% of the surface: %d", vm.PanelW.Get())
	}
	vm.PanelW.Set(10)
	if vm.PanelW.Get() != PanelMinW {
		t.Fatalf("below the minimum: %d", vm.PanelW.Get())
	}
	// A narrower surface does not rewrite the owner's choice, it only limits what
	// is shown; widening it brings the choice back.
	vm.PanelW.Set(700)
	vm.ViewW.Set(500)
	if vm.PanelW.Get() != 700 || vm.EffectivePanelW() != 350 {
		t.Fatalf("chosen %d shown %d", vm.PanelW.Get(), vm.EffectivePanelW())
	}
	vm.ViewW.Set(1000)
	if vm.EffectivePanelW() != 700 {
		t.Fatalf("%d", vm.EffectivePanelW())
	}
}

// open lays the panel out and returns the divider's centre.
func open(t *testing.T, s *Scene) (x, y int) {
	t.Helper()
	s.VM().PanelOpen.Set(true)
	pixels(s)
	hr, ok := s.split.HandleRect(0)
	if !ok {
		t.Fatal("the panel has no divider")
	}
	return center(hr)
}

func TestDraggingThePanelsLeftEdgeResizesItAndStaysInBounds(t *testing.T) {
	s := newScene(t)
	vm := s.VM()
	// Each press is a second apart: two presses close together are a double-click.
	var tick time.Duration
	s.clock = func() time.Time { tick += time.Second; return t0.Add(tick) }
	x, y := open(t, s)
	if s.panel.Bounds().W != PanelDefaultW {
		t.Fatalf("starts at %d", s.panel.Bounds().W)
	}
	drag := func(from, to int) {
		s.HandleClick(from, y)
		s.HandleMove((from+to)/2, y)
		s.HandleMove(to, y)
		s.HandleRelease(to, y)
		pixels(s)
	}
	// Left grows the panel by the distance dragged.
	drag(x, x-100)
	if vm.PanelW.Get() != PanelDefaultW+100 || s.panel.Bounds().W != PanelDefaultW+100 {
		t.Fatalf("dragged 100 left: %d / %d", vm.PanelW.Get(), s.panel.Bounds().W)
	}
	// The divider moved with it; drag it right to shrink.
	x, _ = center(handleRect(t, s))
	drag(x, x+60)
	if vm.PanelW.Get() != PanelDefaultW+40 {
		t.Fatalf("dragged 60 right: %d", vm.PanelW.Get())
	}
	// Far left stops at 70 % of the surface, far right at the minimum.
	x, _ = center(handleRect(t, s))
	drag(x, -4000)
	if want := testW * PanelMaxPercent / 100; vm.PanelW.Get() != want || s.panel.Bounds().W != want {
		t.Fatalf("maximum: %d / %d, want %d", vm.PanelW.Get(), s.panel.Bounds().W, want)
	}
	x, _ = center(handleRect(t, s))
	drag(x, 9000)
	if vm.PanelW.Get() != PanelMinW || s.panel.Bounds().W != PanelMinW {
		t.Fatalf("minimum: %d / %d", vm.PanelW.Get(), s.panel.Bounds().W)
	}
	// The panel never leaves the surface, and wraps its Details at its width.
	if r := s.panel.Bounds(); r.X < 0 || r.X+r.W > testW {
		t.Fatalf("%v", r)
	}
}

func handleRect(t *testing.T, s *Scene) toolkit.Rect {
	t.Helper()
	r, ok := s.split.HandleRect(0)
	if !ok {
		t.Fatal("no divider")
	}
	return r
}

func TestResizeCursorAndHover(t *testing.T) {
	s := newScene(t)
	if s.Cursor(5, 5) != "" || s.HandleMove(5, 5) {
		t.Fatal("no panel, no divider, no cursor")
	}
	x, y := open(t, s)
	if got := s.Cursor(x, y); got != "col-resize" {
		t.Fatalf("over the divider: %q", got)
	}
	if got := s.Cursor(x-300, y); got != "" {
		t.Fatalf("over the graph: %q", got)
	}
	// The pointer arriving on the divider changes the picture once; staying there
	// does not; leaving does.
	if !s.HandleMove(x, y) || s.HandleMove(x, y) {
		t.Fatal("hover on the divider")
	}
	if !s.HandleMove(x-300, y) || s.HandleMove(x-301, y) {
		t.Fatal("hover off the divider")
	}
	// While the divider is held the cursor stays a resize cursor wherever it goes.
	s.HandleClick(x, y)
	if got := s.Cursor(x-200, y); got != "col-resize" {
		t.Fatalf("held: %q", got)
	}
	s.HandleRelease(x-200, y)
	// Closing the panel drops the hover.
	s.HandleMove(x, y)
	s.VM().PanelOpen.Set(false)
	if !s.HandleMove(x, y) || s.HandleMove(x, y) {
		t.Fatal("closing the panel leaves the hover")
	}
}

func TestEscapeInThePanelClosesItAndAnywhereElseFoldsOrgs(t *testing.T) {
	s := newScene(t)
	vm := s.VM()
	open(t, s)
	// The focus is on the graph (a click on a node): Escape does not close the panel.
	box, _ := s.graph.NodeBox("s:bbbbbbbb")
	x, y := center(box)
	s.HandleClick(x, y)
	if !vm.PanelOpen.Get() {
		t.Fatal("selecting opens the panel")
	}
	// Panel widgets are not under the graph now: click the table, then Escape.
	tx, ty := center(s.table.Bounds())
	s.HandleClick(tx, ty)
	if !s.inPanel(s.keyTarget) {
		t.Fatalf("the table does not hold the focus: %T", s.keyTarget)
	}
	if !s.HandleKeyDown("Escape") || vm.PanelOpen.Get() || s.PanelVisible() {
		t.Fatal("Escape with the focus in the panel did not close it")
	}
	// Reopen with the focus on the graph: Escape folds orgs and leaves the panel.
	vm.PanelOpen.Set(true)
	s.keyTarget = s.graph
	s.HandleKeyDown("Escape")
	if !vm.PanelOpen.Get() {
		t.Fatal("Escape on the graph closed the panel")
	}
	// The divider holds the focus after a press on it: Escape closes too.
	dx, dy := center(handleRect(t, s))
	s.HandleClick(dx, dy)
	s.HandleRelease(dx, dy)
	if s.keyTarget != toolkit.Widget(s.split) || !s.inPanel(s.keyTarget) {
		t.Fatal("the divider is part of the panel")
	}
	s.HandleKeyDown("Escape")
	if vm.PanelOpen.Get() {
		t.Fatal("Escape on the divider did not close the panel")
	}
	if s.inPanel(nil) || s.inPanel(s.graph) {
		t.Fatal("inPanel")
	}
	// With nothing focused Escape still reports itself handled.
	s.keyTarget = nil
	if !s.HandleKeyDown("Escape") {
		t.Fatal("Escape")
	}
}

func TestTheCloseButtonRunsTheCommandTheKeyRuns(t *testing.T) {
	s := newScene(t)
	vm := s.VM()
	open(t, s)
	if s.closeBtn.Bounds().W == 0 {
		t.Fatal("the close button is not in the top bar")
	}
	bar := s.panelTitle.Bounds()
	if bar.Y != s.closeBtn.Bounds().Y && bar.Y+bar.H < s.closeBtn.Bounds().Y {
		t.Fatalf("title %v and close %v are not on one bar", bar, s.closeBtn.Bounds())
	}
	x, y := center(s.closeBtn.Bounds())
	s.HandleClick(x, y)
	if vm.PanelOpen.Get() {
		t.Fatal("the close button left the panel open")
	}
	s.HandleClick(x, y) // the panel is gone: this hits the graph, not a ghost button
	if vm.PanelOpen.Get() {
		t.Fatal("a closed panel's button still takes clicks")
	}
}

func TestColumnWidthsAreViewModelStateWithAMinimumPerColumn(t *testing.T) {
	vm := NewViewModel()
	for col, min := range ColMin {
		vm.SetColWidth(col, 1)
		if vm.ColW.At(col) != min {
			t.Errorf("column %d (%s): %d, want its minimum %d", col, ColTitles[col], vm.ColW.At(col), min)
		}
		vm.SetColWidth(col, min+30)
		if vm.ColW.At(col) != min+30 {
			t.Errorf("column %d: %d", col, vm.ColW.At(col))
		}
	}
	vm.SetColWidth(0, 99999)
	if vm.ColW.At(0) != maxColW {
		t.Fatalf("%d", vm.ColW.At(0))
	}
	n := 0
	vm.ColW.SubscribeChanged(func() { n++ })
	vm.SetColWidth(0, maxColW) // unchanged: no event
	vm.SetColWidth(-1, 50)
	vm.SetColWidth(len(ColMin), 50)
	if n != 0 {
		t.Fatal("a no-op or an unknown column changed the widths")
	}
	if ClampColW(-1, 7) != 7 || ClampColW(len(ColMin), 7) != 7 {
		t.Fatal("an unknown column keeps its width")
	}
}

func TestDraggingAColumnSeparatorResizesTheColumnAndClampsToItsMinimum(t *testing.T) {
	s := newScene(t)
	vm := s.VM()
	open(t, s)
	// The table reports a drag in device pixels; the ViewModel holds logical ones,
	// at least the column's minimum, and the table shows what the ViewModel holds.
	s.table.OnColumnResize(colContext, 150)
	if vm.ColW.At(colContext) != 150 || s.table.Columns[colContext].Width != 150 {
		t.Fatalf("%d / %d", vm.ColW.At(colContext), s.table.Columns[colContext].Width)
	}
	s.table.OnColumnResize(colContext, 20)
	if vm.ColW.At(colContext) != ColMin[colContext] || s.table.Columns[colContext].Width != ColMin[colContext] {
		t.Fatalf("below the minimum: %d / %d", vm.ColW.At(colContext), s.table.Columns[colContext].Width)
	}
	// At 2x the same drag is half as many logical pixels.
	s.Resize(2*testW, 2*testH, 2)
	s.table.OnColumnResize(colContext, 300)
	if vm.ColW.At(colContext) != 150 || s.table.Columns[colContext].Width != 300 {
		t.Fatalf("at 2x: %d / %d", vm.ColW.At(colContext), s.table.Columns[colContext].Width)
	}
	// The headers read what the columns are.
	if s.table.Columns[6].Title != "Last" || s.table.Columns[7].Title != "Agents" {
		t.Fatalf("%v", s.table.Columns)
	}
	// A real separator drag on the header goes through the same callback.
	s.Resize(testW, testH, 1)
	hdr := s.table.Bounds()
	sepX := hdr.X
	for i := 0; i <= 1; i++ {
		sepX += s.table.Columns[i].Width
	}
	y := hdr.Y + 8
	before := vm.ColW.At(1)
	s.HandleClick(sepX, y)
	s.HandleMove(sepX+30, y)
	s.HandleRelease(sepX+30, y)
	if vm.ColW.At(1) != before+30 {
		t.Fatalf("a separator drag changed column 1 from %d to %d", before, vm.ColW.At(1))
	}
}

// fakeStore is a Store over a map; Gets and Sets are recorded.
type fakeStore struct {
	m    map[string]string
	sets []string
}

func (f *fakeStore) Get(k string) (string, bool) { v, ok := f.m[k]; return v, ok }
func (f *fakeStore) Set(k, v string)             { f.m[k] = v; f.sets = append(f.sets, k) }

func TestLayoutIsPersistedPerViewerAndRestoredClamped(t *testing.T) {
	st := &fakeStore{m: map[string]string{}}
	s := newScene(t)
	s.SetStore(st)
	if len(st.sets) != 0 {
		t.Fatal("nothing is written before the owner changes something")
	}
	x, y := open(t, s)
	s.HandleClick(x, y)
	s.HandleMove(x-80, y)
	s.HandleRelease(x-80, y)
	if st.m[keyPanelW] != strconv.Itoa(PanelDefaultW+80) {
		t.Fatalf("panel width stored as %q", st.m[keyPanelW])
	}
	s.table.OnColumnResize(colContext, 140)
	if st.m[keyColW] != "56,72,140,68,96,130,40,56" {
		t.Fatalf("columns stored as %q", st.m[keyColW])
	}
	// Another visit restores both.
	s2 := NewScene(testW, testH)
	s2.SetStore(st)
	if s2.VM().PanelW.Get() != PanelDefaultW+80 || s2.VM().ColW.At(colContext) != 140 {
		t.Fatalf("restored %d / %d", s2.VM().PanelW.Get(), s2.VM().ColW.At(colContext))
	}
	// What a store holds is untrusted: garbage is ignored, numbers are clamped.
	for name, c := range map[string]struct{ panel, cols string }{
		"garbage":  {"wide", "a,b"},
		"too few":  {"", "1,2,3"},
		"clamped":  {"99999", "1,1,1,1,1,1,1,99999"},
		"negative": {"-5", "x,1,1,1,1,1,1,1"},
	} {
		bad := &fakeStore{m: map[string]string{keyPanelW: c.panel, keyColW: c.cols}}
		s3 := NewScene(testW, testH)
		s3.SetStore(bad)
		if w := s3.VM().PanelW.Get(); w < PanelMinW || w > testW*PanelMaxPercent/100 {
			t.Fatalf("%s: panel %d out of bounds", name, w)
		}
		for i := range ColMin {
			if got := s3.VM().ColW.At(i); got < ColMin[i] || got > maxColW {
				t.Fatalf("%s: column %d = %d", name, i, got)
			}
		}
	}
	if _, ok := parseColW("1,2,3,4,5,6,7,8"); !ok {
		t.Fatal("eight widths")
	}
	if formatColW([]int{1, 2}) != "1,2" {
		t.Fatal("format")
	}
}

// A store that has nothing leaves the defaults.
func TestAnEmptyStoreKeepsTheDefaults(t *testing.T) {
	s := NewScene(testW, testH)
	s.SetStore(&fakeStore{m: map[string]string{}})
	if s.VM().PanelW.Get() != PanelDefaultW || s.VM().ColW.At(0) != ColDefault[0] {
		t.Fatal("defaults")
	}
}

func TestTheDetailsPaneWrapsAtThePanelWidth(t *testing.T) {
	s := newScene(t)
	vm := s.VM()
	sn := snap()
	sn.Sessions[0].Title = "a title long enough to need wrapping at a narrow panel " + "abcdefghij klmnopqrst uvwxyz 0123456789 abcdefghij klmnopqrst uvwxyz 0123456789"
	vm.Apply(sn)
	vm.Graph.Selected.Set("node:s:aaaaaaaa")
	wide := vm.DetailChars.Get()
	vm.PanelW.Set(PanelMinW)
	narrow := vm.DetailChars.Get()
	if narrow >= wide || narrow < minDetailChars {
		t.Fatalf("%d chars narrow, %d wide", narrow, wide)
	}
	for _, l := range vm.Detail.Slice() {
		if len([]rune(l)) > narrow+2 { // a long unbreakable word may exceed it; ours have spaces
			t.Fatalf("a Details line of %d characters in a panel of %d: %q", len([]rune(l)), narrow, l)
		}
	}
	_ = model.Snapshot{}
}
