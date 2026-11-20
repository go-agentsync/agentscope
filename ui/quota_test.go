package ui

import (
	"strings"
	"testing"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-agentsync/agentscope/quota"
	"github.com/go-widgets/toolkit"
)

const (
	wk     = 7 * 24 * 3600
	reset  = 1795183200 // account A's weekly reset
	resetB = reset + 3*86400 + 7200
)

func series(layers, n int, v float64) [][]float64 {
	out := make([][]float64, layers)
	for f := range out {
		out[f] = make([]float64, n)
		for i := range out[f] {
			out[f][i] = v * float64(f+1)
		}
	}
	return out
}

func account(key, label, plan string, weekEnd int64, sessionPct, weekPct float64, na bool) model.AccountUsage {
	a := model.AccountUsage{
		Key: key, Label: label, Plan: plan, SessionEnd: reset - 3*3600, WeekEnd: weekEnd, ResetSource: "aiquota", RecordingSince: reset - 5*86400,
		Short: model.TokenSeries{Start: reset - 8*3600, Step: 600, Out: series(4, 30, 100), CacheRead: series(4, 30, 5000)},
		Week:  model.TokenSeries{Start: reset - wk, Step: 3600, Out: series(4, 168, 10), CacheRead: series(4, 168, 900)},
		Windows: []model.QuotaWindow{
			{Label: quota.LabelSession, Available: true, Percent: sessionPct, ResetsAt: reset - 3*3600, History: []model.QuotaPoint{{At: reset - 7*3600, Percent: 1}, {At: reset - 4*3600, Percent: 2}}},
			{Label: quota.LabelWeek, Available: true, Percent: weekPct, ResetsAt: weekEnd, History: []model.QuotaPoint{{At: reset - 5*86400, Percent: 40}, {At: reset - 86400, Percent: 54}, {At: reset - 40*86400, Percent: 9}}},
			{Label: "Weekly Sonnet"},
		},
	}
	if na {
		a.Windows[1] = model.QuotaWindow{Label: quota.LabelWeek} // not available
	}
	for k := 0; k < 4; k++ {
		w := model.WeekTotals{Start: weekEnd - int64(4-k)*wk, InProgress: k == 3, Out: []float64{4, 3, 2, 1}, CacheRead: []float64{40, 30, 20, 10}}
		w.End = w.Start + wk
		for i := 0; i < 5+k; i++ {
			w.CumOut = append(w.CumOut, float64(i*(k+1)))
			w.CumCacheRead = append(w.CumCacheRead, float64(i*(k+1)*10))
		}
		w.Recorded, w.Percent = k%2 == 1, float64(10*k)
		a.Weeks = append(a.Weeks, w)
	}
	return a
}

func usageFixture() *model.Usage {
	a := account("aaaa1111", "Claude Code", "max", reset, 2, 54, false)
	b := account("bbbb2222", "Claude Code", "team", resetB, 13, 1, false)
	b.Active = true
	c := account("", "", "", reset-86400, 0, 0, false)
	c.Windows = c.Windows[:1]
	u := &model.Usage{
		At: reset - 3600, Families: model.Families, Active: "bbbb2222",
		Accounts: []model.AccountUsage{a, b, c},
		Capacity: model.Capacity{AccountWeeks: 1.45, Counted: 2, Items: []model.CapacityItem{
			{Key: "aaaa1111", Label: "Claude Code", SessionOK: true, Session: 2, WeeklyOK: true, Weekly: 54, NextReset: reset},
			{Key: "bbbb2222", Label: "Claude Code", Active: true, SessionOK: true, Session: 13, WeeklyOK: true, Weekly: 1, NextReset: resetB},
			{Key: "", Stale: true, Unknown: true},
		}},
		All: model.AllTokens{
			Layers:          append(append([]string(nil), model.Families...), model.UnattributedLayer),
			Short:           model.TokenSeries{Start: reset - 8*3600, Step: 600, Out: series(5, 30, 100), CacheRead: series(5, 30, 5000)},
			Week:            model.TokenSeries{Start: reset - wk, Step: 3600, Out: series(5, 168, 10), CacheRead: series(5, 168, 900)},
			Days:            model.TokenSeries{Start: reset - 28*86400, Step: 86400, Out: series(5, 28, 1e6), CacheRead: series(5, 28, 1e9)},
			UnattributedOut: 2.5e6, UnattributedCache: 3e9,
		},
	}
	return u
}

func TestAccountOptionsNameAccountsByLabelPlanAndShortHashOnly(t *testing.T) {
	u := usageFixture()
	got := AccountOptions(u)
	if len(got) != 4 || got[0] != AllAccounts || got[1] != "Claude Code (max) #aaaa" || got[2] != "Claude Code (team) #bbbb - active" || !strings.HasPrefix(got[3], "unknown account") {
		t.Fatalf("%q", got)
	}
	if o := AccountOptions(nil); len(o) != 1 || o[0] != AllAccounts {
		t.Fatal(o)
	}
	if accountTag("ab", "", "", "") != "account #ab" {
		t.Fatal(accountTag("ab", "", "", ""))
	}
	// An alias replaces the label everywhere an account is named; the unknown
	// account has none.
	if accountTag("ab", "Claude Code", "team", "Labo (team)") != "Labo (team)" || accountTag("", "x", "", "alias") != "unknown account" {
		t.Fatal("alias")
	}
	u.Accounts[0].Alias = "Perso"
	if AccountOptions(u)[1] != "Perso" || accountName(u.Accounts[0]) != "Perso" || markerLabel(u.Accounts[0], " reset") != "Perso reset" {
		t.Fatalf("%q", AccountOptions(u))
	}
}

func TestCapacityRowsCarryTheAliasAndThePlan(t *testing.T) {
	rows := capacityRows(model.Capacity{Items: []model.CapacityItem{
		{Key: "abcd1234", Label: "Claude Code", Alias: "Labo (team)", Plan: "team"},
		{Key: "beef0000", Label: "Claude Code"},
	}})
	if rows[0].Account != "Labo (team)" || rows[0].Plan != "team" || rows[1].Account != "Claude Code #beef" || rows[1].Plan != "-" {
		t.Fatalf("%+v", rows)
	}
	if c := rows[0].Cells(); len(c) != 7 || c[1] != "team" {
		t.Fatalf("%v", c)
	}
}

func TestCapacityRowsSideBySideAndStaleNotCounted(t *testing.T) {
	rows := capacityRows(usageFixture().Capacity)
	if len(rows) != 3 {
		t.Fatal(rows)
	}
	a, b, unk := rows[0], rows[1], rows[2]
	if a.Session != "2 %" || a.Weekly != "54 %" || a.Left != "0.46" || a.Active != "" || a.Resets == "" || a.Resets == "-" {
		t.Fatalf("%+v", a)
	}
	if b.Left != "0.99" || b.Active != "active" || len(b.Cells()) != 7 {
		t.Fatalf("%+v", b)
	}
	if unk.Account != "unknown account" || unk.Left != "not counted" || unk.Resets != "-" || unk.Session != "n/a" {
		t.Fatalf("%+v", unk)
	}
}

func TestBuildQuotaOfOneAccountUsesItsOwnResetsAndListsWhatIsNotAvailable(t *testing.T) {
	u := usageFixture()
	v := buildQuota(u, 1, SpanWeek, KindOutput, false)
	var names []string
	for _, o := range v.Official.Overlays {
		names = append(names, o.Name)
	}
	if len(names) != 1 || names[0] != quota.LabelWeek {
		t.Fatalf("lines %v: the 5 hour window is not on the 7 day chart and N/A is not drawn", names)
	}
	if !strings.Contains(v.Notes, "Weekly Sonnet: not available") {
		t.Fatalf("%q", v.Notes)
	}
	if v.Official.TimeMax-v.Official.TimeMin != wk || len(v.Official.Markers) != 1 || v.Official.Markers[0].At != reset {
		t.Fatalf("%+v", v.Official)
	}
	if got := len(v.Official.Overlays[0].Points); got != 2 {
		t.Fatalf("history outside the span must be dropped, got %d points", got)
	}
	if len(v.Out.Layers) != 4 || len(v.Out.Layers[0]) != 168 || len(v.Names) != 4 || len(v.Colors) != 4 {
		t.Fatal("7 day token series")
	}
	// Account B's week ends later: its own marker is at ITS reset, not A's.
	if mb := buildQuota(u, 2, SpanWeek, KindOutput, false); len(mb.Official.Markers) != 0 || mb.Official.Overlays[0].Points == nil {
		t.Fatalf("B's reset %d is outside the span of this chart: %+v", resetB, mb.Official.Markers)
	}
	fresh := usageFixture()
	fresh.Accounts[0].Windows[1].History = nil
	if n := buildQuota(fresh, 1, SpanWeek, KindOutput, false).Notes; !strings.Contains(n, "a line needs two readings") {
		t.Fatalf("%q", n)
	}
	short := buildQuota(u, 1, SpanShort, KindOutput, true)
	if len(short.Official.Overlays) != 1 || short.Official.Overlays[0].Name != quota.LabelSession || len(short.Out.Layers[0]) != 30 || short.Out.TimeMax-short.Out.TimeMin != 5*3600 {
		t.Fatalf("%+v", short.Official)
	}
	if len(short.Official.Markers) != 1 || !strings.HasPrefix(short.Official.Markers[0].Label, "reset ") {
		t.Fatalf("%+v", short.Official.Markers)
	}
	if short.Official.FormatTime(0) == "" || v.Official.FormatTime(0) == "" {
		t.Fatal("time formats")
	}
}

func TestBuildQuotaOfOneAccountWeeksCompareFourWeeksAndMarkWhatIsMissing(t *testing.T) {
	u := usageFixture()
	out := buildQuota(u, 1, SpanWeeks, KindOutput, false)
	cr := buildQuota(u, 1, SpanWeeks, KindCacheRead, false)
	a := u.Accounts[0]
	if len(out.WeekBars.Layers) != 4 || len(out.WeekBars.Layers[0]) != 4 || out.WeekBars.Layers[0][0] != 4 || cr.WeekBars.Layers[0][0] != 40 {
		t.Fatalf("%v / %v", out.WeekBars.Layers, cr.WeekBars.Layers)
	}
	if out.WeekBars.TimeMin != a.Weeks[0].Start || out.WeekBars.TimeMax != a.Weeks[3].End || out.WeekBars.FormatTime(0) == "" {
		t.Fatal("bars span the four weeks")
	}
	if len(out.Cum.Overlays) != 4 || !strings.Contains(out.Cum.Overlays[3].Name, "in progress") || out.Cum.Overlays[2].Name != "last week" || out.Cum.Overlays[0].Name != "3 weeks ago" {
		t.Fatalf("%+v", out.Cum.Overlays)
	}
	if first := out.Cum.Overlays[0].Points[0]; first.At != 0 || first.Value != 0 {
		t.Fatalf("every line starts at day 0, 0: %+v", first)
	}
	if out.Cum.TimeMax != wk || out.Cum.FormatTime(2*86400) != "day 2" || out.Cum.Max < 28 || cr.Cum.Max <= out.Cum.Max {
		t.Fatalf("%+v", out.Cum)
	}
	if len(out.WeekPct.Points) != 2 || len(out.WeekPct.Markers) != 4 {
		t.Fatalf("recorded weekly %% with the 4 resets marked: %+v", out.WeekPct)
	}
	if !strings.Contains(out.Notes, "not recorded") || !strings.Contains(out.Notes, "10 %") || !strings.Contains(out.Notes, "(in progress)") || strings.Contains(out.Notes, "recording since") {
		t.Fatalf("%q", out.Notes)
	}

	// No weekly history yet: say since when it is recorded, never draw it.
	u2 := usageFixture()
	u2.Accounts[0].Windows[1].History = nil
	if n := buildQuota(u2, 1, SpanWeeks, KindOutput, false).Notes; !strings.Contains(n, "recording since") {
		t.Fatalf("%q", n)
	}
	u2.Accounts[0].RecordingSince = 0
	if n := buildQuota(u2, 1, SpanWeeks, KindOutput, true).Notes; !strings.Contains(n, "no history recorded yet") {
		t.Fatalf("%q", n)
	}
	u2.Accounts[0].Weeks = nil
	if v := buildQuota(u2, 1, SpanWeeks, KindOutput, false); len(v.WeekBars.Layers) != 0 {
		t.Fatal("no weeks, no bars")
	}
}

func TestBuildQuotaAggregatedDrawsEveryAccountWithItsOwnResets(t *testing.T) {
	u := usageFixture()
	w := buildQuota(u, 0, SpanWeek, KindOutput, false)
	if len(w.Official.Overlays) != 3-1 { // the unknown account has no weekly window here
		t.Fatalf("%d lines", len(w.Official.Overlays))
	}
	if w.Official.Overlays[0].Name != "account #aaaa" || w.Official.Overlays[1].Name != "account #bbbb" || w.Official.Overlays[0].Ink == w.Official.Overlays[1].Ink {
		t.Fatalf("each account its own named, coloured line: %+v", w.Official.Overlays)
	}
	if len(w.Official.Markers) != 1 || w.Official.Markers[0].At != reset {
		t.Fatalf("B's reset is after the 7 days drawn; A's is the one marker: %+v", w.Official.Markers)
	}
	if len(w.Out.Layers) != 5 || len(w.Names) != 5 || w.Names[4] != model.UnattributedLayer || len(w.Colors) != 5 {
		t.Fatal("the aggregated bars carry the unattributed layer")
	}
	if !strings.Contains(w.Notes, "capacity left: 1.45 account-weeks (2 of 3 accounts counted)") || !strings.Contains(w.Notes, "unattributed") || !strings.Contains(w.Notes, "2.5M") {
		t.Fatalf("%q", w.Notes)
	}
	s := buildQuota(u, 0, SpanShort, KindOutput, true)
	if len(s.Official.Overlays) != 3 || len(s.Official.Markers) != 3 || len(s.Out.Layers[0]) != 30 {
		t.Fatalf("%d lines %d markers", len(s.Official.Overlays), len(s.Official.Markers))
	}

	// 4 x 7 days, aggregated: calendar days, and each account's own resets one
	// week apart.
	d := buildQuota(u, 0, SpanWeeks, KindOutput, false)
	if len(d.WeekBars.Layers) != 5 || len(d.WeekBars.Layers[0]) != 28 || d.WeekBars.TimeMax-d.WeekBars.TimeMin != 28*86400 {
		t.Fatalf("28 calendar days: %d layers", len(d.WeekBars.Layers))
	}
	byInk := map[toolkit.RGBA]int{}
	for _, m := range d.WeekPct.Markers {
		byInk[m.Ink]++
	}
	if len(byInk) != 2 {
		t.Fatalf("two accounts, two marker series: %v", byInk)
	}
	for _, n := range byInk {
		if n < 3 {
			t.Fatalf("each account's resets step back by 7 days across the span: %v", byInk)
		}
	}
	dc := buildQuota(u, 0, SpanWeeks, KindCacheRead, false)
	if dc.WeekBars.Layers[0][0] <= d.WeekBars.Layers[0][0] {
		t.Fatal("the kind selects the cache-read tokens")
	}

	// An account whose weekly window is not available is listed, not drawn.
	u.Accounts[1] = account("bbbb2222", "Claude Code", "team", resetB, 13, 1, true)
	na := buildQuota(u, 0, SpanWeek, KindOutput, false)
	if len(na.Official.Overlays) != 1 || !strings.Contains(na.Notes, "#bbbb") || !strings.Contains(na.Notes, "not available") {
		t.Fatalf("%q", na.Notes)
	}
	// No account has a window: say so.
	empty := &model.Usage{Families: model.Families, All: usageFixture().All, QuotaErr: "aiquota not found"}
	if n := buildQuota(empty, 0, SpanWeek, KindOutput, false).Notes; !strings.Contains(n, "no account has a recorded") || !strings.Contains(n, "aiquota: aiquota not found") {
		t.Fatalf("%q", n)
	}
	// An out-of-range selection is the aggregate.
	if v := buildQuota(u, 9, SpanWeek, KindOutput, false); len(v.Names) != 5 {
		t.Fatal("selection out of range")
	}
}

func TestBuildQuotaSaysWhereTheResetCameFromAndWhatFailed(t *testing.T) {
	u := usageFixture()
	u.Accounts[0].ResetSource, u.QuotaErr = "recorded", "aiquota not found"
	n := buildQuota(u, 1, SpanWeek, KindOutput, false).Notes
	if !strings.Contains(n, "aiquota: aiquota not found") || !strings.Contains(n, "last recorded reset") {
		t.Fatalf("%q", n)
	}
	u.Accounts[0].ResetSource = ""
	if n := buildQuota(u, 1, SpanWeek, KindOutput, false).Notes; !strings.Contains(n, "no weekly reset known") {
		t.Fatalf("%q", n)
	}
	if v := buildQuota(nil, 0, SpanWeek, KindOutput, false); v.Notes == "" || len(v.Out.Layers) != 0 {
		t.Fatal("no usage yet")
	}
}

func TestTokensAndColours(t *testing.T) {
	for in, want := range map[float64]string{0: "0", 999: "999", 1500: "2k", 2.5e6: "2.5M", 3e9: "3.0G"} {
		if got := Tokens(in); got != want {
			t.Errorf("Tokens(%v) = %q, want %q", in, got, want)
		}
	}
	if len(StackColors(false)) != len(model.Families) || len(lineColors(true)) < 5 || len(stackColorsAll(true)) != 5 {
		t.Fatal("palettes")
	}
	seen := map[[3]uint8]bool{}
	for _, c := range stackColorsAll(false) {
		seen[[3]uint8{c.R, c.G, c.B}] = true
	}
	if len(seen) != 5 {
		t.Fatal("two layers share a colour")
	}
	if clock(0) == "" || dayTime(0) == "" || date(0) == "" {
		t.Fatal("time formats")
	}
}
