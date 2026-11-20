package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-agentsync/agentscope/quota"
	"github.com/go-widgets/toolkit"
)

// The spans the Quota tab can show, in the order of the selector.
const (
	SpanShort = iota // the 5 hour window
	SpanWeek         // the 7 day window
	SpanWeeks        // four 7 day windows, aligned on the weekly reset
)

// The token kinds the weeks view can show, in the order of the selector.
const (
	KindOutput = iota
	KindCacheRead
)

// SpanNames and KindNames label the selectors.
var (
	SpanNames = []string{"5 h", "7 days", "4 x 7 days"}
	KindNames = []string{"output", "cache read"}
)

const (
	day      = 24 * 3600
	cumStep  = 6 * 3600
	weekSecs = 7 * day
)

// lineData is what a TimeSeriesChart shows.
type lineData struct {
	Points           []toolkit.TimePoint
	Overlays         []toolkit.TimeLine
	Markers          []toolkit.TimeMarker
	TimeMin, TimeMax int64
	Max              float64
	Percent          bool
	FormatTime       func(int64) string
}

// barData is what a StackedBarChart shows.
type barData struct {
	Layers           [][]float64
	TimeMin, TimeMax int64
	FormatTime       func(int64) string
}

// quotaView is the Quota tab's charts for one span and kind.
type quotaView struct {
	Official lineData // 5 h and 7 days: the official windows
	Out      barData  // 5 h and 7 days: output tokens
	Cache    barData  // 5 h and 7 days: cache-read tokens
	WeekBars barData  // 4 x 7 days: tokens per week
	Cum      lineData // 4 x 7 days: cumulative tokens by day
	WeekPct  lineData // 4 x 7 days: the recorded official weekly percentage
	Notes    string
}

// StackColors are the families' colours, in the order of model.Usage.Families:
// four of the Okabe-Ito set, which stays apart under colour-vision deficiency.
func StackColors(dark bool) []toolkit.RGBA {
	p := Palette(dark)
	return []toolkit.RGBA{p[4], p[0], p[2], p[6]}
}

// lineColors are the colours of the official windows and of the weekly lines.
func lineColors(dark bool) []toolkit.RGBA {
	p := Palette(dark)
	return []toolkit.RGBA{Brand, p[5], p[2], p[6], p[7]}
}

// Tokens writes a token count as "1.2M" or "340k".
func Tokens(v float64) string {
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%.1fG", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%.0fk", v/1e3)
	}
	return fmt.Sprintf("%.0f", v)
}

func clock(at int64) string   { return time.Unix(at, 0).Format("15:04") }
func dayTime(at int64) string { return time.Unix(at, 0).Format("Mon Jan 2 15:04") }
func date(at int64) string    { return time.Unix(at, 0).Format("Jan 2 15:04") }

// buildQuota turns the server's usage view into charts. It never draws a window
// that is not available, and never fills a gap in the recorded history.
func buildQuota(u *model.Usage, span, kind int, dark bool) quotaView {
	var v quotaView
	if u == nil {
		v.Notes = "reading the transcripts..."
		return v
	}
	cols := lineColors(dark)
	var notes []string

	// The official windows over the span, one line each; those that are not
	// available are listed, not drawn.
	switch span {
	case SpanShort, SpanWeek:
		start, end, fmtT := u.Short.Start, u.Short.Start+5*3600, clock
		if span == SpanWeek {
			start, end, fmtT = u.Week.Start, u.Week.Start+weekSecs, dayTime
		}
		v.Official = lineData{TimeMin: start, TimeMax: end, Max: 100, Percent: true, FormatTime: fmtT}
		n := 0
		for _, w := range u.Windows {
			isShort := w.Label == quota.LabelSession
			if isShort != (span == SpanShort) {
				continue
			}
			if !w.Available {
				notes = append(notes, w.Label+": not available")
				continue
			}
			pts := pointsOf(w.History, start, end)
			v.Official.Overlays = append(v.Official.Overlays, toolkit.TimeLine{Name: w.Label, Ink: cols[n%len(cols)], Points: pts})
			n++
			if w.ResetsAt >= start && w.ResetsAt <= end {
				v.Official.Markers = append(v.Official.Markers, toolkit.TimeMarker{At: w.ResetsAt, Label: "reset " + fmtT(w.ResetsAt), Ink: cols[1]})
			}
		}
		for _, w := range u.Windows {
			if w.Available && (w.Label == quota.LabelSession) == (span == SpanShort) && len(w.History) < 2 {
				notes = append(notes, "recorded history of "+w.Label+": "+recordingSince(u)+" (a line needs two readings)")
			}
		}
		v.Out = barData{Layers: u.Short.Out, TimeMin: start, TimeMax: end, FormatTime: fmtT}
		v.Cache = barData{Layers: u.Short.CacheRead, TimeMin: start, TimeMax: end, FormatTime: fmtT}
		if span == SpanWeek {
			v.Out.Layers, v.Cache.Layers = u.Week.Out, u.Week.CacheRead
		}
	default:
		v.buildWeeks(u, kind, cols, &notes)
	}

	if u.QuotaErr != "" {
		notes = append(notes, "aiquota: "+u.QuotaErr)
	}
	switch u.ResetSource {
	case "recorded":
		notes = append(notes, "weeks aligned on the last recorded reset")
	case "":
		notes = append(notes, "no weekly reset known: the weeks end now")
	}
	v.Notes = strings.Join(notes, "  |  ")
	return v
}

// pointsOf keeps the history points inside [start, end].
func pointsOf(h []model.QuotaPoint, start, end int64) []toolkit.TimePoint {
	var out []toolkit.TimePoint
	for _, p := range h {
		if p.At >= start && p.At <= end {
			out = append(out, toolkit.TimePoint{At: p.At, Value: p.Percent})
		}
	}
	return out
}

// buildWeeks fills the 4 x 7 days view: tokens per week, the cumulative lines,
// and the recorded weekly percentage.
func (v *quotaView) buildWeeks(u *model.Usage, kind int, cols []toolkit.RGBA, notes *[]string) {
	if len(u.Weeks) == 0 {
		return
	}
	first, last := u.Weeks[0].Start, u.Weeks[len(u.Weeks)-1].End
	layers := make([][]float64, len(u.Families))
	for f := range layers {
		for _, w := range u.Weeks {
			vals := w.Out
			if kind == KindCacheRead {
				vals = w.CacheRead
			}
			layers[f] = append(layers[f], vals[f])
		}
	}
	v.WeekBars = barData{Layers: layers, TimeMin: first, TimeMax: last, FormatTime: date}

	// Cumulative lines: this week and the three before it, over days 0 to 7.
	v.Cum = lineData{TimeMin: 0, TimeMax: weekSecs, FormatTime: func(at int64) string { return fmt.Sprintf("day %d", at/day) }}
	var peak float64
	names := []string{"3 weeks ago", "2 weeks ago", "last week", "this week"}
	for k, w := range u.Weeks {
		cum := w.CumOut
		if kind == KindCacheRead {
			cum = w.CumCacheRead
		}
		pts := []toolkit.TimePoint{{At: 0, Value: 0}}
		for i, c := range cum {
			pts = append(pts, toolkit.TimePoint{At: int64(i+1) * cumStep, Value: c})
			peak = max(peak, c)
		}
		name := names[(len(names)-len(u.Weeks)+k+len(names))%len(names)]
		if w.InProgress {
			name = "this week (in progress)"
		}
		v.Cum.Overlays = append(v.Cum.Overlays, toolkit.TimeLine{Name: name, Ink: cols[(len(u.Weeks)-1-k)%len(cols)], Points: pts})
	}
	v.Cum.Max = toolkit.NiceCeiling(peak)

	// The recorded weekly percentage, with the resets marked.
	v.WeekPct = lineData{TimeMin: first, TimeMax: last, Max: 100, Percent: true, FormatTime: date}
	var pts []toolkit.TimePoint
	for _, w := range u.Windows {
		if w.Label == quota.LabelWeek {
			pts = pointsOf(w.History, first, last)
		}
	}
	v.WeekPct.Points = pts
	for _, w := range u.Weeks {
		v.WeekPct.Markers = append(v.WeekPct.Markers, toolkit.TimeMarker{At: w.Start, Ink: cols[1]})
	}
	if len(pts) < 2 {
		*notes = append(*notes, "official weekly %: "+recordingSince(u))
	}
	var per []string
	for _, w := range u.Weeks {
		s := "not recorded"
		if w.Recorded {
			s = fmt.Sprintf("%.0f %%", w.Percent)
		}
		if w.InProgress {
			s += " (in progress)"
		}
		per = append(per, s)
	}
	*notes = append(*notes, "official weekly % per week, oldest first: "+strings.Join(per, ", "))
}

// recordingSince says since when the official readings are recorded.
func recordingSince(u *model.Usage) string {
	if u.RecordingSince > 0 {
		return "recording since " + date(u.RecordingSince)
	}
	return "no history recorded yet"
}
