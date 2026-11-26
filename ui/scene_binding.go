package ui

import (
	"github.com/go-widgets/toolkit"
)

// This file is where the Scene sets the widget fields that follow the metric
// scale or the quota data: appearance configuration the toolkit has no
// observable for. Everything the user changes or the collector feeds goes
// through the ViewModel instead.

// tableColumns are the sessions table's columns at a scale, with the widths the
// owner has dragged them to (logical pixels, one per column; the defaults when
// there are none). They are device pixels (the Table does not scale a column's
// width). The table scrolls sideways when they add up to more than the panel.
func tableColumns(scale float64, widths []int) []toolkit.TableColumn {
	titles := ColTitles()
	cols := make([]toolkit.TableColumn, len(titles))
	for i, t := range titles {
		lw := ColDefault[i]
		if i < len(widths) {
			lw = widths[i]
		}
		cols[i] = toolkit.TableColumn{Title: t, Width: int(float64(ClampColW(i, lw))*scale + 0.5)}
	}
	return cols
}

// applyMetrics sets what depends on the scale.
func (s *Scene) applyMetrics() {
	s.graph.NodeWidth = NodeWidth // logical: the graph scales it itself
	s.graph.CornerRadius = s.px(nodeRound)
	s.table.Columns = tableColumns(s.scale, s.vm.ColW.Slice())
	s.title.FontSize = s.px(20)
	s.capTable.Columns = capColumns(s.scale)
	s.gTable.Columns = guardColumns(s.scale)
	s.ctxTable.Columns = []toolkit.TableColumn{{Title: T("ctx.of"), Width: s.px(170)}, {Title: T("ctx.context")}}
}

// capColumns are the capacity table's columns, in device pixels.
func capColumns(scale float64) []toolkit.TableColumn {
	w := func(v int) int { return int(float64(v)*scale + 0.5) }
	return []toolkit.TableColumn{
		{Title: T("cap.account")}, {Title: T("cap.plan"), Width: w(70)}, {Title: T("cap.5h"), Width: w(70)}, {Title: T("cap.weekly"), Width: w(70)},
		{Title: T("cap.weeksleft"), Width: w(90)}, {Title: T("cap.reset"), Width: w(170)}, {Title: T("cap.active"), Width: w(70)},
	}
}

// refreshQuota draws the Quota tab's charts from the usage, the span and the
// kind, and says what is missing in the notes.
func (s *Scene) refreshQuota() {
	// The charts of a tab nobody is looking at are not rebuilt, and nothing is
	// repainted for them; the first time the tab is shown they are.
	if s.vm.Tab.Get() != TabQuota {
		s.quotaStale = true
		return
	}
	s.quotaStale = false
	dark := s.vm.Dark.Get()
	u := s.vm.Usage.Get()
	v := buildQuota(u, s.vm.Account.Get(), s.vm.Span.Get(), s.vm.Kind.Get(), dark)
	setLines(s.official, v.Official)
	setLines(s.cum, v.Cum)
	setLines(s.weekPct, v.WeekPct)
	setBars(s.outBars, v.Out, v)
	setBars(s.cacheBars, v.Cache, v)
	setBars(s.weekBars, v.WeekBars, v)
	s.vm.QuotaNotes.Set(v.Notes)
	s.dirty = true
}

func setLines(c *toolkit.TimeSeriesChart, d lineData) {
	c.Points, c.Overlays, c.Markers = d.Points, d.Overlays, d.Markers
	c.TimeMin, c.TimeMax = d.TimeMin, d.TimeMax
	c.Min, c.Max = 0, d.Max
	if d.Max <= 0 {
		c.Max = 1
	}
	c.FormatTime = d.FormatTime
	c.FormatValue = Tokens
	if d.Percent {
		c.FormatValue = func(v float64) string { return Tokens(v) + " %" }
	}
}

func setBars(c *toolkit.StackedBarChart, d barData, v quotaView) {
	c.Layers, c.Names, c.Colors = d.Layers, v.Names, v.Colors
	c.TimeMin, c.TimeMax = d.TimeMin, d.TimeMax
	c.FormatTime = d.FormatTime
	c.FormatValue = Tokens
}
