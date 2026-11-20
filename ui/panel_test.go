package ui

import (
	"testing"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/toolkit"
)

func TestPanelIsClosedByDefaultAndTheGraphTakesTheSurface(t *testing.T) {
	s := newScene(t)
	if s.PanelVisible() || len(s.overlay.Layers) != 0 {
		t.Fatal("the side panel starts closed")
	}
	g := s.graph.Bounds()
	if g.W < testW-4*margin-40 || g.H < testH/2 {
		t.Fatalf("the graph does not use the surface: %v on %dx%d", g, testW, testH)
	}
	if g.X+g.W > testW || g.Y+g.H > testH {
		t.Fatalf("the graph overflows the surface: %v", g)
	}
}

func TestPanelOpensWithTheKeyTheButtonAndASelection(t *testing.T) {
	s := newScene(t)
	vm := s.VM()
	// The key.
	if !s.HandleChar("t") || !vm.PanelOpen.Get() || len(s.overlay.Layers) != 1 {
		t.Fatal("t did not open the panel")
	}
	s.HandleChar("t")
	if vm.PanelOpen.Get() || len(s.overlay.Layers) != 0 {
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
	// A selection opens it, and shows the details there.
	box, _ := s.graph.NodeBox("s:bbbbbbbb")
	x, y := center(box)
	s.HandleClick(x, y)
	if !vm.PanelOpen.Get() || vm.Detail.At(0) != "session "+idB {
		t.Fatalf("selecting a node must open the panel on its details: open=%v %q", vm.PanelOpen.Get(), vm.Detail.At(0))
	}
	// Typing "t" into the search box is typing, not a toggle.
	sb := s.search.Bounds()
	s.HandleClick(sb.X+10, sb.Y+5)
	s.HandleChar("t")
	if !vm.PanelOpen.Get() || vm.Query.Get() != "t" {
		t.Fatalf("t in the search box toggled the panel (query %q)", vm.Query.Get())
	}
}

func TestPanelOverlaysTheGraphAndSwallowsItsClicks(t *testing.T) {
	s := newScene(t)
	s.VM().PanelOpen.Set(true)
	pb := s.panel.Bounds()
	gb := s.graph.Bounds()
	if pb.X+pb.W != gb.X+gb.W+s.px(cardPad/2)+1 && pb.X+pb.W > testW {
		t.Fatalf("the panel is not right-aligned over the graph: %v / %v", pb, gb)
	}
	if want := s.px(panelW); pb.W != want {
		t.Fatalf("panel is %d wide, want %d", pb.W, want)
	}
	// A click on the panel's own chrome does not reach the node beneath.
	s.VM().Graph.Selected.Set("")
	before := s.VM().Graph.Selected.Get()
	if !s.HandleClick(pb.X+2, pb.Y+2) {
		t.Fatal("click")
	}
	if s.VM().Graph.Selected.Get() != before {
		t.Fatal("a click on the panel reached the graph")
	}
	// The wheel goes to the widget under the pointer: the table, not the graph.
	tb := s.table.Bounds()
	if !s.HandleScroll(tb.X+5, tb.Y+tb.H-3, 3) || !s.HandleScroll(1, testH-1, 3) {
		t.Fatal("scroll")
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
	if s.PanelVisible() || len(s.overlay.Layers) != 0 {
		t.Fatal("the sessions panel floats over the graph, not over the charts")
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
	if pw := s.panel.Bounds().W; pw != 2*s.px(panelW)/2*1 && pw != s.px(panelW) {
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
	if len(s.targets()) < 6 {
		t.Fatal("the span selectors take clicks on the Quota tab")
	}
	click := func(r *toolkit.RadioButton) { x, y := center(r.Bounds()); s.HandleClick(x, y) }
	click(s.spanRadios[SpanWeeks])
	if vm.Span.Get() != SpanWeeks || s.quota.Visible().Get() != "weeks" {
		t.Fatalf("span %d, layout %q", vm.Span.Get(), s.quota.Visible().Get())
	}
	click(s.kindRadios[KindCacheRead])
	if vm.Kind.Get() != KindCacheRead {
		t.Fatal("the kind radio did not drive the view model")
	}
	click(s.spanRadios[SpanShort])
	if vm.Span.Get() != SpanShort || s.quota.Visible().Get() != "range" || len(s.targets()) != 4+3 {
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
