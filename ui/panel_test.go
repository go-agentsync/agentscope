package ui

import (
	"testing"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/toolkit"
)

func TestPanelIsClosedByDefaultAndTheGraphTakesTheSurface(t *testing.T) {
	s := newScene(t)
	if s.PanelVisible() || s.panel.Bounds().W != 0 {
		t.Fatal("the side panel starts closed, and takes no room")
	}
	g := s.graph.Bounds()
	if g.W < testW-4*margin-40 || g.H < testH/2 {
		t.Fatalf("the graph does not use the surface: %v on %dx%d", g, testW, testH)
	}
	if g.X+g.W > testW || g.Y+g.H > testH {
		t.Fatalf("the graph overflows the surface: %v", g)
	}
}

func TestPanelOpensWithTheKeyAndTheButtonButNotWithASelection(t *testing.T) {
	s := newScene(t)
	vm := s.VM()
	// The key.
	if !s.HandleChar("t") || !vm.PanelOpen.Get() || s.panel.Bounds().W == 0 {
		t.Fatal("t did not open the panel")
	}
	s.HandleChar("t")
	if vm.PanelOpen.Get() || s.panel.Bounds().W != 0 {
		t.Fatal("t did not close it")
	}
	// The header button.
	bx, by := center(s.toggle.Bounds())
	s.HandleClick(bx, by)
	if !vm.PanelOpen.Get() || !s.PanelVisible() {
		t.Fatal("the button did not open the panel")
	}
	s.HandleClick(bx, by)
	if vm.PanelOpen.Get() {
		t.Fatal("the button did not close it")
	}
	// A selection does not open it: the panel is docked, and opening it would move
	// the node that was just clicked. Its details are there when it is opened.
	box, _ := s.graph.NodeBox("s:bbbbbbbb")
	x, y := center(box)
	s.HandleClick(x, y)
	if vm.PanelOpen.Get() {
		t.Fatal("selecting a node opened the docked panel")
	}
	if after, _ := s.graph.NodeBox("s:bbbbbbbb"); after != box {
		t.Fatalf("the clicked node moved from %v to %v", box, after)
	}
	s.HandleChar("t")
	if vm.Detail.At(0) != "session "+idB {
		t.Fatalf("%q", vm.Detail.At(0))
	}
	// Typing "t" into the search box is typing, not a toggle.
	sb := s.search.Bounds()
	s.HandleClick(sb.X+10, sb.Y+5)
	s.HandleChar("t")
	if !vm.PanelOpen.Get() || vm.Query.Get() != "t" {
		t.Fatalf("t in the search box toggled the panel (query %q)", vm.Query.Get())
	}
}

// The panel is DOCKED: opening it takes its width from the graph, which lays
// itself out again in what is left; closing it gives the width back; dragging the
// divider moves the edge between them.
func TestPanelIsDockedBesideTheGraphAndNeverCoversIt(t *testing.T) {
	s := newScene(t)
	vm := s.VM()
	closed := s.graph.Bounds()
	vm.PanelOpen.Set(true)
	pb, gb := s.panel.Bounds(), s.graph.Bounds()
	if want := s.px(PanelDefaultW); pb.W != want {
		t.Fatalf("panel is %d wide, want %d", pb.W, want)
	}
	if gb.X+gb.W > pb.X {
		t.Fatalf("the graph %v reaches under the panel %v", gb, pb)
	}
	if gb.W >= closed.W || closed.W-gb.W < pb.W {
		t.Fatalf("opening the panel did not shrink the graph by its width: %v -> %v, panel %v", closed, gb, pb)
	}
	// The graph lays out again into the narrower rectangle: every cluster is inside.
	for _, p := range toolkit.LayoutProblems(s.graph) {
		t.Fatal(p)
	}
	vm.PanelOpen.Set(false)
	if s.graph.Bounds() != closed || s.panel.Bounds().W != 0 {
		t.Fatalf("closing did not give the width back: %v vs %v", s.graph.Bounds(), closed)
	}
	// A click on the panel's own chrome does not reach the graph.
	vm.PanelOpen.Set(true)
	pb = s.panel.Bounds()
	vm.Graph.Selected.Set("")
	s.HandleClick(pb.X+2, pb.Y+2)
	if vm.Graph.Selected.Get() != "" {
		t.Fatal("a click on the panel reached the graph")
	}
	// The wheel goes to the widget under the pointer: the table, not the graph.
	tb := s.table.Bounds()
	if !s.HandleScroll(tb.X+5, tb.Y+tb.H-3, 3) || !s.HandleScroll(1, testH-1, 3) {
		t.Fatal("scroll")
	}
}

// With the panel open as wide as it may be, on a 1024 px window, every cluster of
// the graph lies inside the graph's rectangle, and the graph keeps a minimum width
// beside the panel. This is the case that covering the graph made invisible.
func TestOpenAtMaximumWidthOn1024EveryClusterIsInsideTheGraphPane(t *testing.T) {
	defer toolkit.SetMetricScale(toolkit.MetricScale())
	s := NewScene(1024, 768)
	vm := s.VM()
	vm.Apply(withUsage(crowded()))
	vm.PanelOpen.Set(true)
	vm.PanelW.Set(100000) // as wide as the bounds allow
	pixels(s)
	if vm.PanelW.Get() != 1024*PanelMaxPercent/100 {
		t.Fatalf("maximum is %d", vm.PanelW.Get())
	}
	gb, pb := s.graph.Bounds(), s.panel.Bounds()
	if gb.W < GraphMinW-2*s.px(cardPad) || gb.X+gb.W > pb.X {
		t.Fatalf("graph %v beside panel %v: it keeps a minimum width and ends where the panel starts", gb, pb)
	}
	if ps := toolkit.LayoutProblems(s.graph); len(ps) != 0 {
		t.Fatalf("a cluster or node lies outside the graph pane: %v", ps[0])
	}
	for _, n := range vm.Graph.Nodes.Slice() { // and every node box is inside the pane too
		if box, ok := s.graph.NodeBox(n.ID); ok && (box.X < gb.X || box.X+box.W > gb.X+gb.W) {
			t.Fatalf("node %s %v is outside the graph %v", n.ID, box, gb)
		}
	}
	// A wider window gives the graph more columns back: the narrower rectangle is a
	// re-layout, and closing the panel restores the first layout exactly.
	vm.PanelOpen.Set(false)
	pixels(s)
	if s.graph.Bounds().W <= gb.W {
		t.Fatal("closing the panel did not widen the graph")
	}
}

func TestPanelIsNotShownOnTheQuotaTab(t *testing.T) {
	s := newScene(t)
	s.VM().PanelOpen.Set(true)
	// Click the second tab.
	tb := s.tabs.Bounds()
	s.HandleClick(tb.X+s.px(toolkit.NotebookTabWidth)*3/2, tb.Y+tb.H/2)
	if s.VM().Tab.Get() != TabQuota {
		t.Fatalf("tab %d", s.VM().Tab.Get())
	}
	if s.PanelVisible() {
		t.Fatal("the sessions panel is not shown over the charts")
	}
	if s.pages.Visible().Get() != "quota" {
		t.Fatal("the quota page is not shown")
	}
	s.VM().Tab.Set(TabGraph)
	if !s.PanelVisible() || s.pages.Visible().Get() != "graph" {
		t.Fatal("back on the graph the panel returns as it was")
	}
}

func TestResizeRelaysOutAndUsesTheWholeSurface(t *testing.T) {
	defer toolkit.SetMetricScale(toolkit.MetricScale())
	s := newScene(t)
	narrow := s.graph.Bounds()
	rows := func() int { // distinct tops of the cluster columns' first nodes
		seen := map[int]bool{}
		for _, n := range s.VM().Graph.Nodes.Slice() {
			if b, ok := s.graph.NodeBox(n.ID); ok {
				seen[b.Y] = true
			}
		}
		return len(seen)
	}
	s.Resize(500, 700, 1)
	narrowRows := rows()
	s.Resize(3000, 1200, 1)
	if w, h := s.Size(); w != 3000 || h != 1200 {
		t.Fatal(w, h)
	}
	wide := s.graph.Bounds()
	if wide.W <= narrow.W || wide.H <= narrow.H {
		t.Fatalf("the graph did not grow with the surface: %v -> %v", narrow, wide)
	}
	if rows() >= narrowRows {
		t.Fatalf("a wider surface must hold more cluster columns: %d rows narrow, %d wide", narrowRows, rows())
	}
	buf := make([]byte, 4*3000*1200)
	s.Draw(buf)

	// A device pixel ratio of 2 doubles the node and the panel.
	s.Resize(1600, 900, 1)
	one, _ := s.graph.NodeBox("s:bbbbbbbb")
	s.Resize(3200, 1800, 2)
	two, _ := s.graph.NodeBox("s:bbbbbbbb")
	if two.W != 2*one.W || two.H != 2*one.H {
		t.Fatalf("at 2x a node is %dx%d, at 1x %dx%d", two.W, two.H, one.W, one.H)
	}
	s.VM().PanelOpen.Set(true)
	if pw := s.panel.Bounds().W; pw != s.px(PanelDefaultW) {
		t.Fatalf("panel %d", pw)
	}

	// Ignored: nothing to draw into.
	s.Resize(0, 10, 1)
	s.Resize(10, 10, 0)
	if w, _ := s.Size(); w != 3200 {
		t.Fatal("an unusable size was taken")
	}
}

func TestAnEdgeUpdateNeverMovesTheLayout(t *testing.T) {
	s := newScene(t)
	s.Resize(1400, 800, 1)
	before := map[string]toolkit.Rect{}
	for _, n := range s.VM().Graph.Nodes.Slice() {
		before[n.ID], _ = s.graph.NodeBox(n.ID)
	}
	s.VM().ShowExpired.Set(true)
	s.VM().Graph.Selected.Set("node:s:aaaaaaaa") // changes the edges only
	pixels(s)
	for id, b := range before {
		if got, _ := s.graph.NodeBox(id); got != b {
			t.Fatalf("%s moved from %v to %v on an edge update", id, b, got)
		}
	}
}

func TestQuotaTabSelectorsDriveTheViewModel(t *testing.T) {
	s := newScene(t)
	vm := s.VM()
	vm.Apply(withUsage(snap()))
	vm.Tab.Set(TabQuota)
	pixels(s)
	if len(s.targets()) < 8 {
		t.Fatal("the span selectors take clicks on the Quota tab")
	}
	click := func(r *toolkit.RadioButton) { x, y := center(r.Bounds()); s.HandleClick(x, y) }
	click(s.spanRadios[SpanWeeks])
	if vm.Span.Get() != SpanWeeks || s.quota.Visible().Get() != "allweeks" {
		t.Fatalf("span %d, layout %q", vm.Span.Get(), s.quota.Visible().Get())
	}
	click(s.kindRadios[KindCacheRead])
	if vm.Kind.Get() != KindCacheRead {
		t.Fatal("the kind radio did not drive the view model")
	}
	click(s.spanRadios[SpanShort])
	if vm.Span.Get() != SpanShort || s.quota.Visible().Get() != "allrange" || len(s.targets()) != 4+3+3 {
		t.Fatal("the 5 h span uses the range layout and has no kind selector")
	}
	buf := pixels(s)
	if buf[0] == 0 && buf[1] == 0 {
		t.Fatal("nothing drawn")
	}
	vm.Dark.Set(true) // the charts follow the theme
	pixels(s)
}

func withUsage(sn model.Snapshot) model.Snapshot {
	sn.Usage = usageFixture()
	return sn
}
