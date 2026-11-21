package ui

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

func guardScene(t *testing.T) *Scene {
	t.Helper()
	s := newScene(t)
	s.VM().Apply(snapWith(guardFixture()))
	s.VM().Tab.Set(TabGuardrails)
	return s
}

func TestGuardrailsTabIsTheThirdAndShowsItsPage(t *testing.T) {
	s := guardScene(t)
	if s.pages.Visible().Get() != "guardrails" || s.PanelVisible() {
		t.Fatalf("page %q", s.pages.Visible().Get())
	}
	s.VM().PanelOpen.Set(true)
	if s.PanelVisible() {
		t.Fatal("the sessions panel is not shown over the Guardrails tab")
	}
	// A click on the third tab selects it.
	s.VM().Tab.Set(TabGraph)
	tb := s.tabs.Bounds()
	s.HandleClick(tb.X+s.px(toolkit.NotebookTabWidth)*5/2, tb.Y+tb.H/2)
	if s.VM().Tab.Get() != TabGuardrails {
		t.Fatalf("tab %d", s.VM().Tab.Get())
	}
	s.VM().Tab.Set(TabQuota)
	if s.pages.Visible().Get() != "quota" {
		t.Fatal("the Quota tab is still reachable")
	}
	// A tab past the last is held on the last.
	s.VM().Tab.Set(9)
	if s.pages.Visible().Get() != "guardrails" {
		t.Fatalf("%q", s.pages.Visible().Get())
	}
}

func TestGuardrailsWidgetsFollowTheViewModelAndDrawInBothThemes(t *testing.T) {
	s := guardScene(t)
	vm := s.VM()
	if len(s.gTable.Rows) != 5 || s.gChips[0].Text != "untriaged 1" || s.gChips[3].Text != "refusals 6 this week, 3 last" {
		t.Fatalf("%d rows, chips %q", len(s.gTable.Rows), s.gChips[0].Text)
	}
	if len(s.gRec.Overlays) != 2 || len(s.gRec.Markers) != 2 || len(s.gBars.Layers) != 3 || len(s.gBars.Names) != 3 {
		t.Fatal("the charts do not show the data")
	}
	if s.gNotes.Text().Get() == "" {
		t.Fatal("no notes")
	}
	vm.Guard.Sel.Set(1)
	if len(s.gDetail.Lines()) == 0 {
		t.Fatal("the detail pane is empty for a selected lesson")
	}
	// The decision filter drives the table.
	vm.Guard.Decision.Set(1)
	if len(s.gTable.Rows) != 3 {
		t.Fatalf("%d rows", len(s.gTable.Rows))
	}
	vm.Guard.Decision.Set(0)
	pixels(s)
	vm.Dark.Set(true) // the charts and the fills follow the theme
	pixels(s)
	// Data gone: charts empty, nothing leaks through.
	vm.Apply(snapWith(nil))
	if len(s.gRec.Overlays) != 0 || len(s.gBars.Layers) != 0 || len(s.gTable.Rows) != 0 {
		t.Fatal("charts of data that is gone")
	}
	pixels(s)
}

func TestAHeaderClickSortsTheTableThroughTheViewModel(t *testing.T) {
	s := guardScene(t)
	g := s.VM().Guard
	if g.SortCol.Get() != GColAfter || g.SortAsc.Get() {
		t.Fatal("the table starts on the leaking gates")
	}
	if s.gTable.SortColumn().Get() != GColAfter || s.gTable.SortAsc().Get() {
		t.Fatal("the table's arrow does not show the order")
	}
	pixels(s)
	b := s.gTable.Bounds()
	// The first column's title is at the left of the header row.
	s.HandleClick(b.X+s.px(10), b.Y+s.px(8))
	if g.SortCol.Get() != GColName || !g.SortAsc.Get() {
		t.Fatalf("column %d ascending=%v", g.SortCol.Get(), g.SortAsc.Get())
	}
	if g.Rows.At(0).Name != "leaky" || g.Rows.At(4).Name != "wait" {
		t.Fatalf("%v", names(g))
	}
	s.HandleClick(b.X+s.px(10), b.Y+s.px(8))
	if g.SortAsc.Get() || g.Rows.At(0).Name != "wait" {
		t.Fatalf("a second click reverses: %v", names(g))
	}
}

func TestALeakingLessonIsRedAndASilentGateAmber(t *testing.T) {
	s := guardScene(t)
	rows := s.VM().Guard.Rows.Slice()
	at := func(name string) int {
		for i, r := range rows {
			if r.Name == name {
				return i
			}
		}
		t.Fatalf("no row %s", name)
		return -1
	}
	for _, dark := range []bool{false, true} {
		s.VM().Dark.Set(dark)
		if c, ok := s.gTable.CellFill(at("leaky"), GColAfter); !ok || c != PressureColor(4, dark) {
			t.Fatalf("a gate that leaks is not red (dark=%v): %v %v", dark, c, ok)
		}
		if c, ok := s.gTable.CellFill(at("silent"), GColFlag); !ok || c != PressureColor(2, dark) {
			t.Fatalf("a silent gate is not flagged (dark=%v)", dark)
		}
	}
	for _, c := range []struct {
		name string
		col  int
	}{{"plain", GColAfter}, {"leaky", GColFlag}, {"leaky", GColName}, {"silent", GColAfter}} {
		if _, ok := s.gTable.CellFill(at(c.name), c.col); ok {
			t.Errorf("%s column %d is coloured", c.name, c.col)
		}
	}
	if _, ok := s.gTable.CellFill(-1, GColAfter); ok {
		t.Fatal("a fill for no row")
	}
	if _, ok := s.gTable.CellFill(99, GColAfter); ok {
		t.Fatal("a fill for a row that does not exist")
	}
}

func TestGuardrailsTabTakesClicksOnItsOwnWidgetsOnly(t *testing.T) {
	s := guardScene(t)
	pixels(s)
	targets := s.targets()
	has := func(w toolkit.Widget) bool {
		for _, t := range targets {
			if t == w {
				return true
			}
		}
		return false
	}
	if !has(s.gDecision) || !has(s.gTable) || !has(s.gDetail) || has(s.capTable) || has(s.graph) {
		t.Fatal("the Guardrails tab's click targets are wrong")
	}
	// The decision drop-down opens, takes the next click, and chooses.
	x, y := center(s.gDecision.Bounds())
	s.HandleClick(x, y)
	if !s.gDecision.PopoverOpen() {
		t.Fatal("the decision list did not open")
	}
	s.HandleClick(1, 1) // a click elsewhere closes it
	if s.gDecision.PopoverOpen() {
		t.Fatal("the open list did not take the click")
	}
	// The wheel reaches the table.
	tx, ty := center(s.gTable.Bounds())
	if !s.HandleScroll(tx, ty, 1) {
		t.Fatal("the wheel was not handled")
	}
}
