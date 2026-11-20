package ui

import (
	"strconv"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/toolkit"
)

// CtxRow is one gauge of the Details pane: a session or a sub-agent and how full
// its context is.
type CtxRow struct {
	Name  string
	Text  string  // "212k / 300k"
	Ratio float64 // context / window, 0 when unknown
	Band  int     // model.PressureBand of Ratio
}

// Cells are the row as the Details table shows it; the gauge is drawn under the
// text of the second cell.
func (r CtxRow) Cells() []string { return []string{r.Name, r.Text} }

// ctxOf is the gauge of a context of ctx tokens against a window.
func ctxOf(name string, ctx, window int64) CtxRow {
	r := model.ContextRatio(ctx, window)
	return CtxRow{Name: name, Text: contextText(ctx, window), Ratio: r, Band: model.PressureBand(r)}
}

// contextText is "212k / 300k", or "- / 300k" for a context nobody saw.
func contextText(ctx, window int64) string {
	if ctx <= 0 {
		return "- / " + tokenCount(window)
	}
	return tokenCount(ctx) + " / " + tokenCount(window)
}

// tokenCount is a token count in thousands ("212k") or millions ("1M", "1.5M").
func tokenCount(n int64) string {
	switch {
	case n >= 1_000_000:
		whole, tenth := n/1_000_000, (n%1_000_000+50_000)/100_000
		if tenth == 10 {
			whole, tenth = whole+1, 0
		}
		if tenth == 0 {
			return strconv.FormatInt(whole, 10) + "M"
		}
		return strconv.FormatInt(whole, 10) + "." + strconv.FormatInt(tenth, 10) + "M"
	case n >= 1000:
		return strconv.FormatInt((n+500)/1000, 10) + "k"
	}
	return strconv.FormatInt(n, 10)
}

// pressure holds the five bands' colours, in model.PressureBand order: dark
// green, light green, yellow, orange, red. The green pair is told apart by
// lightness, and every band keeps its contrast against the table's track in the
// Slate theme, light and dark; the sets are not the same: a light ground wants
// the saturated end, a dark one the lifted end.
var pressure = [2][model.Bands]toolkit.RGBA{
	{ // light theme
		toolkit.RGB(0x15, 0x80, 0x3D), toolkit.RGB(0x4A, 0xDE, 0x80), toolkit.RGB(0xEA, 0xB3, 0x08), toolkit.RGB(0xEA, 0x58, 0x0C), toolkit.RGB(0xDC, 0x26, 0x26),
	},
	{ // dark theme
		toolkit.RGB(0x16, 0xA3, 0x4A), toolkit.RGB(0x86, 0xEF, 0xAC), toolkit.RGB(0xFD, 0xE0, 0x47), toolkit.RGB(0xFB, 0x92, 0x3C), toolkit.RGB(0xF8, 0x71, 0x71),
	},
}

// PressureColor is the colour of a pressure band in the light or the dark theme.
func PressureColor(band int, dark bool) toolkit.RGBA {
	i := 0
	if dark {
		i = 1
	}
	return pressure[i][min(max(band, 0), model.Bands-1)]
}
