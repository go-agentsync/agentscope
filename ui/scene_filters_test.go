package ui

import (
	"testing"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/toolkit"
)

func filterScene(t *testing.T) *Scene {
	t.Helper()
	s := newScene(t)
	s.VM().Apply(filterSnap())
	s.VM().PanelOpen.Set(true)
	pixels(s)
	return s
}

func TestFilterWidgetsDriveTheViewModelAndTheTable(t *testing.T) {
	s := filterScene(t)
	vm := s.VM()
	if len(s.table.Rows) != 5 {
		t.Fatalf("%d rows", len(s.table.Rows))
	}
	s.stateDD.Select(1) // Active
	if vm.Filters.State.Get() != StateActive || len(s.table.Rows) != 2 {
		t.Fatalf("state %d, %d rows", vm.Filters.State.Get(), len(s.table.Rows))
	}
	s.lastDD.Select(1) // < 10 min
	s.repoDD.Select(1) // the first repository
	if vm.Filters.Last.Get() != 1 || vm.Filters.Repo.Get() != 1 || len(s.table.Rows) != 1 {
		t.Fatalf("%d %d %d", vm.Filters.Last.Get(), vm.Filters.Repo.Get(), len(s.table.Rows))
	}
	// The model check boxes are clickable and bound.
	if len(s.shownChecks) == 0 {
		t.Fatal("no model check boxes on a panel with room")
	}
	x, y := center(s.shownChecks[0].Bounds())
	s.HandleClick(x, y)
	if len(vm.Filters.models) != 1 {
		t.Fatalf("a click on a check box did not choose its model: %v", vm.Filters.models)
	}
	// Clear filters, through its button.
	cx, cy := center(s.clearBtn.Bounds())
	s.HandleClick(cx, cy)
	if vm.Filters.Active() || len(s.table.Rows) != 5 {
		t.Fatal("Clear filters did not clear")
	}
	if got := vm.Status.Get(); got[:len("5 of 5 sessions shown")] != "5 of 5 sessions shown" {
		t.Fatalf("%q", got)
	}
	pixels(s)
}

// A short window has no room for the model check boxes: they are left out of the
// layout and out of the clickable targets, and nothing overflows.
func TestShortWindowDropsTheModelRowsNotTheFilters(t *testing.T) {
	s := NewScene(1280, 520)
	s.VM().Apply(filterSnap())
	s.VM().PanelOpen.Set(true)
	pixels(s)
	if len(s.shownChecks) != 0 {
		t.Fatalf("%d check boxes shown with no room", len(s.shownChecks))
	}
	for _, w := range s.targets() {
		for _, c := range s.modelChecks {
			if w == toolkit.Widget(c) {
				t.Fatal("a check box that was not laid out takes clicks")
			}
		}
	}
	if got := outside(s); len(got) != 0 {
		t.Fatalf("%v", got[0])
	}
}

func TestGaugesAreDrawnUnderTheTextInThePressureColour(t *testing.T) {
	s := newScene(t)
	vm := s.VM()
	vm.Apply(ctxSnap())
	vm.PanelOpen.Set(true)
	vm.Graph.Selected.Set("node:s:aaaaaaaa")
	pixels(s)
	f, c, ok := s.table.CellBar(0, colContext)
	if !ok || f != 0.75 || c != PressureColor(model.BandHot, false) {
		t.Fatalf("row 0: %v %v %v", f, c, ok)
	}
	if _, _, ok := s.table.CellBar(0, 0); ok {
		t.Fatal("only the Context column has a gauge")
	}
	if _, _, ok := s.table.CellBar(99, colContext); ok {
		t.Fatal("a row that is not there")
	}
	if _, _, ok := s.table.CellBar(-1, colContext); ok {
		t.Fatal("a row that is not there")
	}
	vm.Dark.Set(true)
	if _, c, _ := s.table.CellBar(1, colContext); c != PressureColor(model.BandCalm, true) {
		t.Fatalf("dark theme: %v", c)
	}
	// The Details gauges: the session and each sub-agent, in the second column.
	if f, c, ok := s.ctxTable.CellBar(0, 1); !ok || f != 0.75 || c != PressureColor(model.BandHot, true) {
		t.Fatalf("%v %v %v", f, c, ok)
	}
	if _, _, ok := s.ctxTable.CellBar(0, 0); ok {
		t.Fatal("the name column has no gauge")
	}
	if _, _, ok := s.ctxTable.CellBar(9, 1); ok {
		t.Fatal("no such gauge")
	}
	if len(s.ctxTable.Rows) != 3 {
		t.Fatalf("%d gauges", len(s.ctxTable.Rows))
	}
	// A session whose context nobody saw has a text and an empty bar, not a gap.
	if f, _, ok := s.table.CellBar(2, colContext); !ok || f != 0 {
		t.Fatalf("%v %v", f, ok)
	}
	pixels(s)
}
