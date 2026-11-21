package ui

import (
	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/mvvmtk"
	"github.com/go-widgets/toolkit"
)

// guardColumns are the lessons table's columns, in device pixels; the lesson and
// the gate share what the others leave.
func guardColumns(scale float64) []toolkit.TableColumn {
	w := func(v int) int { return int(float64(v)*scale + 0.5) }
	widths := []int{0, w(54), w(92), w(76), 0, w(130), w(76)}
	cols := make([]toolkit.TableColumn, len(GuardTitles))
	for i, t := range GuardTitles {
		cols[i] = toolkit.TableColumn{Title: t, Width: widths[i], Sortable: true}
	}
	return cols
}

// buildGuardWidgets makes the Guardrails tab's widgets; their data and bindings
// come from bindGuard and refreshGuard.
func (s *Scene) buildGuardWidgets() {
	s.gRec = toolkit.NewTimeSeriesChart(nil, 0, 1)
	s.gBars = toolkit.NewStackedBarChart(nil, nil)
	s.gTable = toolkit.NewTable(guardColumns(1), nil)
	// A click selects a row only while MultiSelect is on (go-widgets/toolkit#595).
	s.gTable.MultiSelect = true
	s.gDetail = toolkit.NewWrapList(nil)
	s.gDecision = toolkit.NewDropDown(DecisionNames, 0)
	for i := range s.gChips {
		s.gChips[i] = toolkit.NewChip("")
		s.gChips[i].Style = toolkit.ChipPlain
	}
	s.gNotes = toolkit.NewLabel("")
}

// bindGuard connects the Guardrails widgets to the ViewModel.
func (s *Scene) bindGuard(invalidate func()) {
	g := s.vm.Guard
	s.unbind = append(s.unbind,
		mvvmtk.BindTableRows(s.gTable, g.Rows, GuardRow.Cells, invalidate),
		mvvmtk.BindTableSelection(s.gTable, g.Sel, invalidate),
		bindWrapList(s.gDetail, g.Detail, invalidate),
		mvvmtk.BindSelectedIndex(s.gDecision, g.Decision, invalidate),
		mvvmtk.BindLabel(s.gNotes, g.Notes, invalidate),
		g.Data.SubscribeChanged(func() { s.refreshGuard(); invalidate() }),
	)
	for i, c := range s.gChips {
		s.unbind = append(s.unbind, bindChip(c, g.Chips[i], invalidate))
	}
	// A header click orders the table: the ViewModel holds the order and sorts.
	s.gTable.OnSort = g.SetSort
	s.gTable.SortColumn().Set(g.SortCol.Get())
	s.gTable.SortAsc().Set(g.SortAsc.Get())
	// A lesson with a recurrence after its decision is red; a silent gate is amber.
	s.gTable.CellFill = func(row, col int) (toolkit.RGBA, bool) {
		if row < 0 || row >= g.Rows.Len() {
			return toolkit.RGBA{}, false
		}
		r := g.Rows.At(row)
		switch {
		case col == GColAfter && r.After > 0:
			return PressureColor(model.Bands-1, s.vm.Dark.Get()), true
		case col == GColFlag && r.Silent:
			return PressureColor(2, s.vm.Dark.Get()), true
		}
		return toolkit.RGBA{}, false
	}
}

// refreshGuard draws the Guardrails tab's charts from the data.
func (s *Scene) refreshGuard() {
	v := buildGuard(s.vm.Guard.Data.Get(), s.vm.Dark.Get())
	setLines(s.gRec, v.Rec)
	setBars(s.gBars, v.Bars, quotaView{Names: v.Names, Colors: v.Colors})
	s.dirty = true
}

// guardPage is the Guardrails tab: the headline chips and the decision filter over
// the two trend charts, over the lessons table and the selected lesson's lines.
func (s *Scene) guardPage() toolkit.Widget {
	px := s.px
	top := toolkit.NewHBox()
	top.Spacing = px(gap / 2)
	for _, c := range s.gChips {
		top.AddFlex(c, 1)
	}
	top.AddFixed(toolkit.NewLabel("decision"), px(72))
	top.AddFixed(s.gDecision, px(150))

	charts := toolkit.NewHBox()
	charts.Spacing = px(gap)
	charts.AddFlex(card("Recurrences per week (dashed lines: a gate landed)", s.gRec, cardPad), 1)
	charts.AddFlex(card("Gate refusals per week, by gate", s.gBars, cardPad), 1)

	lower := toolkit.NewHBox()
	lower.Spacing = px(gap)
	lower.AddFlex(card("Lessons", s.gTable, cardPad), 3)
	lower.AddFlex(card("Selected lesson", s.gDetail, cardPad), 2)

	page := toolkit.NewVBox()
	page.Spacing = px(gap)
	page.AddFixed(top, px(controlsH))
	page.AddFlex(charts, 2)
	page.AddFlex(lower, 3)
	page.AddFixed(s.gNotes, px(toolkit.StatusbarH))
	return page
}
