package ui

import (
	"fmt"
	"strings"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-agentsync/agentscope/quota"
	"github.com/go-widgets/toolkit"
)

// AllAccounts is the first option of the account selector: every account,
// aggregated.
const AllAccounts = "All (aggregated)"

// accountName is how an account is named on screen: the label aiquota gives it,
// its plan and a short hash of its key. Never its e-mail or its id, which the
// server does not even send.
func accountName(a model.AccountUsage) string {
	if a.Key == "" {
		return "unknown account (recorded before accounts were told apart)"
	}
	return accountTag(a.Key, a.Label, a.Plan, a.Alias)
}

// accountTag names an account: the display name its owner gave it (an alias
// replaces the label everywhere), else "label (plan) #hash".
func accountTag(key, label, plan, alias string) string {
	if key == "" {
		return "unknown account"
	}
	if alias != "" {
		return alias
	}
	if label == "" {
		label = "account"
	}
	if plan != "" {
		label += " (" + plan + ")"
	}
	return label + " #" + key[:min(4, len(key))]
}

// AccountOptions are the selector's options: All, then every account, the active
// one marked.
func AccountOptions(u *model.Usage) []string {
	out := []string{AllAccounts}
	if u == nil {
		return out
	}
	for _, a := range u.Accounts {
		n := accountName(a)
		if a.Active {
			n += " - active"
		}
		out = append(out, n)
	}
	return out
}

// CapRow is one line of the capacity table.
type CapRow struct {
	Account, Plan, Session, Weekly, Left, Resets, Active string
}

// Cells are the row's cells.
func (r CapRow) Cells() []string {
	return []string{r.Account, r.Plan, r.Session, r.Weekly, r.Left, r.Resets, r.Active}
}

func pct(ok bool, v float64) string {
	if !ok {
		return "n/a"
	}
	return fmt.Sprintf("%.0f %%", v)
}

// capacityRows lists each account's 5 hour and weekly percentages side by side,
// what is left of its week, when that resets, and which account is active. An
// account whose reading is older than its reset is listed as stale, not counted.
func capacityRows(c model.Capacity) []CapRow {
	var rows []CapRow
	for _, it := range c.Items {
		plan := it.Plan
		if plan == "" {
			plan = "-"
		}
		r := CapRow{Account: accountTag(it.Key, it.Label, "", it.Alias), Plan: plan, Session: pct(it.SessionOK, it.Session), Weekly: pct(it.WeeklyOK, it.Weekly), Left: "-", Resets: "-"}
		switch {
		case it.Unknown:
			r.Left = "not counted"
		case it.Stale:
			r.Weekly = "stale"
		case it.WeeklyOK:
			r.Left = fmt.Sprintf("%.2f", (100-it.Weekly)/100)
			r.Resets = dayTime(it.NextReset)
		}
		if it.Active {
			r.Active = "active"
		}
		rows = append(rows, r)
	}
	return rows
}

// grey is the unattributed layer: a neutral that is no family's colour.
func stackColorsAll(dark bool) []toolkit.RGBA {
	return append(StackColors(dark), Palette(dark)[7])
}

// buildQuota turns the server's usage view into charts: of one account (sel is
// its 1-based position in u.Accounts) or of all of them (sel 0).
func buildQuota(u *model.Usage, sel, span, kind int, dark bool) quotaView {
	if u == nil {
		return quotaView{Notes: "reading the transcripts..."}
	}
	var v quotaView
	if sel <= 0 || sel > len(u.Accounts) {
		v = buildAll(u, span, kind, dark)
	} else {
		v = buildAccountView(u.Accounts[sel-1], u.Families, u.QuotaErr, span, kind, dark)
		v.Names, v.Colors = u.Families, StackColors(dark)
	}
	return v
}

// buildAll is the aggregated view. Accounts have different reset anchors, so the
// 4 x 7 days span is aligned on calendar days and each account's resets are its
// own marker series; the 5 h and 7 day spans draw every account's window as its
// own line.
func buildAll(u *model.Usage, span, kind int, dark bool) quotaView {
	var v quotaView
	cols := lineColors(dark)
	v.Names, v.Colors = u.All.Layers, stackColorsAll(dark)
	var notes []string

	var start, end int64
	var fmtT func(int64) string
	switch span {
	case SpanShort:
		start, end, fmtT = u.All.Short.Start, u.All.Short.Start+5*3600, clock
		v.Out = barData{Layers: u.All.Short.Out, TimeMin: start, TimeMax: end, FormatTime: fmtT}
		v.Cache = barData{Layers: u.All.Short.CacheRead, TimeMin: start, TimeMax: end, FormatTime: fmtT}
	case SpanWeek:
		start, end, fmtT = u.All.Week.Start, u.All.Week.Start+weekSecs, dayTime
		v.Out = barData{Layers: u.All.Week.Out, TimeMin: start, TimeMax: end, FormatTime: fmtT}
		v.Cache = barData{Layers: u.All.Week.CacheRead, TimeMin: start, TimeMax: end, FormatTime: fmtT}
	default:
		start, end, fmtT = u.All.Days.Start, u.All.Days.Start+int64(len(u.All.Days.Out[0]))*day, date
		layers := u.All.Days.Out
		if kind == KindCacheRead {
			layers = u.All.Days.CacheRead
		}
		v.WeekBars = barData{Layers: layers, TimeMin: start, TimeMax: end, FormatTime: fmtT}
	}

	label := quota.LabelWeek
	if span == SpanShort {
		label = quota.LabelSession
	}
	v.Official = lineData{TimeMin: start, TimeMax: end, Max: 100, Percent: true, FormatTime: fmtT}
	if span == SpanWeeks {
		v.WeekPct = v.Official
	}
	target := &v.Official
	if span == SpanWeeks {
		target = &v.WeekPct
	}
	for i, a := range u.Accounts {
		ink := cols[i%len(cols)]
		for _, w := range a.Windows {
			if w.Label != label {
				continue
			}
			if !w.Available {
				notes = append(notes, accountName(a)+": "+w.Label+" not available")
				continue
			}
			target.Overlays = append(target.Overlays, toolkit.TimeLine{Name: accountTag(a.Key, "", "", a.Alias), Ink: ink, Points: pointsOf(w.History, start, end)})
			if span != SpanWeeks {
				if w.ResetsAt >= start && w.ResetsAt <= end {
					target.Markers = append(target.Markers, toolkit.TimeMarker{At: w.ResetsAt, Label: markerLabel(a, " reset"), Ink: ink})
				}
				continue
			}
			// Each account's own resets, one week apart, as its marker series.
			for k := int64(0); k <= 4 && w.ResetsAt > 0; k++ {
				at := w.ResetsAt - k*weekSecs
				if at >= start && at <= end {
					m := toolkit.TimeMarker{At: at, Ink: ink}
					if k == 0 || at-weekSecs < start {
						m.Label = markerLabel(a, "")
					}
					target.Markers = append(target.Markers, m)
				}
			}
		}
	}
	if len(target.Overlays) == 0 {
		notes = append(notes, "no account has a recorded "+label+" window")
	}

	if u.All.UnattributedOut > 0 || u.All.UnattributedCache > 0 {
		notes = append(notes, fmt.Sprintf("unattributed (no account recorded before the call), last 28 days: %s output, %s cache-read", Tokens(u.All.UnattributedOut), Tokens(u.All.UnattributedCache)))
	}
	notes = append(notes, fmt.Sprintf("capacity left: %.2f account-weeks (%d of %d accounts counted)", u.Capacity.AccountWeeks, u.Capacity.Counted, len(u.Capacity.Items)))
	if u.QuotaErr != "" {
		notes = append(notes, "aiquota: "+u.QuotaErr)
	}
	v.Notes = strings.Join(notes, "  |  ")
	return v
}

// markerLabel names a reset marker after its account, except the unknown one,
// whose text would only crowd the chart.
func markerLabel(a model.AccountUsage, suffix string) string {
	if a.Key == "" {
		return ""
	}
	return accountTag(a.Key, "", "", a.Alias) + suffix
}
