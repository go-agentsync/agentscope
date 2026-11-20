package ui

import (
	"github.com/go-widgets/toolkit"
)

// This file is where the Scene sets the widget fields that follow the metric
// scale or the quota data: appearance configuration the toolkit has no
// observable for. Everything the user changes or the collector feeds goes
// through the ViewModel instead.

// tableColumns are the sessions table's columns at a scale. They are device
// pixels (the Table does not scale a column's width), and sized for the side
// panel's width; the task column takes what is left.
func tableColumns(scale float64) []toolkit.TableColumn {
	w := func(v int) int { return int(float64(v)*scale + 0.5) }
	return []toolkit.TableColumn{
		{Title: "State", Width: w(48)},
		{Title: "Session", Width: w(62)},
		{Title: "Model", Width: w(62)},
		{Title: "Repository", Width: w(96)},
		{Title: "Task"},
		{Title: "Last", Width: w(34)},
		{Title: "Ag", Width: w(24)},
	}
}

// applyMetrics sets what depends on the scale.
func (s *Scene) applyMetrics() {
	s.graph.NodeWidth = NodeWidth // logical: the graph scales it itself
	s.graph.CornerRadius = s.px(nodeRound)
	s.table.Columns = tableColumns(s.scale)
	s.title.FontSize = s.px(20)
	s.capTable.Columns = capColumns(s.scale)
}

// capColumns are the capacity table's columns, in device pixels.
func capColumns(scale float64) []toolkit.TableColumn {
	w := func(v int) int { return int(float64(v)*scale + 0.5) }
	return []toolkit.TableColumn{
		{Title: "Account"}, {Title: "5 h", Width: w(80)}, {Title: "Weekly", Width: w(80)},
		{Title: "Weeks left", Width: w(100)}, {Title: "Weekly reset", Width: w(180)}, {Title: "Active", Width: w(80)},
	}
}

// refreshQuota draws the Quota tab's charts from the usage, the span and the
// kind, and says what is missing in the notes.
func (s *Scene) refreshQuota() {
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
