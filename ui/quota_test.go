package ui

import (
	"strings"
	"testing"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-agentsync/agentscope/quota"
)

const (
	wk    = 7 * 24 * 3600
	reset = 1795183200 // the weekly reset
)

func series(n int, v float64) [][]float64 {
	out := make([][]float64, len(model.Families))
	for f := range out {
		out[f] = make([]float64, n)
		for i := range out[f] {
			out[f][i] = v * float64(f+1)
		}
	}
	return out
}

func usageFixture() *model.Usage {
	u := &model.Usage{
		At: reset - 3600, SessionEnd: reset - 3*3600, WeekEnd: reset, ResetSource: "aiquota", RecordingSince: reset - 5*86400,
		Families: model.Families,
		Short:    model.TokenSeries{Start: reset - 8*3600, Step: 600, Out: series(30, 100), CacheRead: series(30, 5000)},
		Week:     model.TokenSeries{Start: reset - wk, Step: 3600, Out: series(168, 10), CacheRead: series(168, 900)},
		Windows: []model.QuotaWindow{
			{Label: quota.LabelSession, Available: true, Percent: 2, ResetsAt: reset - 3*3600, History: []model.QuotaPoint{{At: reset - 7*3600, Percent: 1}, {At: reset - 4*3600, Percent: 2}}},
			{Label: quota.LabelWeek, Available: true, Percent: 54, ResetsAt: reset, History: []model.QuotaPoint{{At: reset - 5*86400, Percent: 40}, {At: reset - 86400, Percent: 54}, {At: reset - 40*86400, Percent: 9}}},
			{Label: "Weekly Sonnet"},
			{Label: "Weekly Fable", Available: true, ResetsAt: reset, History: []model.QuotaPoint{{At: reset - 86400, Percent: 0}}},
		},
	}
	for k := 0; k < 4; k++ {
		w := model.WeekTotals{Start: reset - int64(4-k)*wk, InProgress: k == 3, Out: []float64{4, 3, 2, 1}, CacheRead: []float64{40, 30, 20, 10}}
		w.End = w.Start + wk
		for i := 0; i < 5+k; i++ {
			w.CumOut = append(w.CumOut, float64(i*(k+1)))
			w.CumCacheRead = append(w.CumCacheRead, float64(i*(k+1)*10))
		}
		w.Recorded, w.Percent = k%2 == 1, float64(10*k)
		u.Weeks = append(u.Weeks, w)
	}
	return u
}

func TestBuildQuotaRangeViewsListWhatIsNotAvailableAndDrawNoZeroForIt(t *testing.T) {
	u := usageFixture()
	v := buildQuota(u, SpanWeek, KindOutput, false)
	var names []string
	for _, o := range v.Official.Overlays {
		names = append(names, o.Name)
	}
	if len(names) != 2 || names[0] != quota.LabelWeek || names[1] != "Weekly Fable" {
		t.Fatalf("lines %v: the session window is not on the 7 day chart and N/A is not drawn", names)
	}
	if !strings.Contains(v.Notes, "Weekly Sonnet: not available") {
		t.Fatalf("%q", v.Notes)
	}
	if v.Official.TimeMax-v.Official.TimeMin != wk || !v.Official.Percent || len(v.Official.Markers) != 2 {
		t.Fatalf("%+v", v.Official)
	}
	if got := len(v.Official.Overlays[0].Points); got != 2 {
		t.Fatalf("history outside the span must be dropped, got %d points", got)
	}
	if len(v.Out.Layers) != 4 || len(v.Out.Layers[0]) != 168 || v.Out.TimeMax-v.Out.TimeMin != wk {
		t.Fatal("7 day token series")
	}
	short := buildQuota(u, SpanShort, KindOutput, true)
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

func TestBuildQuotaWeeksCompareFourWeeksAndMarkWhatIsMissing(t *testing.T) {
	u := usageFixture()
	out := buildQuota(u, SpanWeeks, KindOutput, false)
	cr := buildQuota(u, SpanWeeks, KindCacheRead, false)
	if len(out.WeekBars.Layers) != 4 || len(out.WeekBars.Layers[0]) != 4 || out.WeekBars.Layers[0][0] != 4 || cr.WeekBars.Layers[0][0] != 40 {
		t.Fatalf("%v / %v", out.WeekBars.Layers, cr.WeekBars.Layers)
	}
	if out.WeekBars.TimeMin != u.Weeks[0].Start || out.WeekBars.TimeMax != u.Weeks[3].End || out.WeekBars.FormatTime(0) == "" {
		t.Fatal("bars span the four weeks")
	}
	if len(out.Cum.Overlays) != 4 || !strings.Contains(out.Cum.Overlays[3].Name, "in progress") || out.Cum.Overlays[2].Name != "last week" || out.Cum.Overlays[0].Name != "3 weeks ago" {
		t.Fatalf("%+v", out.Cum.Overlays)
	}
	if first := out.Cum.Overlays[0].Points[0]; first.At != 0 || first.Value != 0 {
		t.Fatalf("every line starts at day 0, 0: %+v", first)
	}
	if out.Cum.TimeMax != wk || out.Cum.FormatTime(2*86400) != "day 2" || out.Cum.Max < 28 {
		t.Fatalf("%+v", out.Cum)
	}
	if cr.Cum.Max <= out.Cum.Max {
		t.Fatal("the kind selects the cumulative lines too")
	}
	if len(out.WeekPct.Points) != 2 || len(out.WeekPct.Markers) != 4 {
		t.Fatalf("recorded weekly %% with the 4 resets marked: %+v", out.WeekPct)
	}
	if !strings.Contains(out.Notes, "not recorded") || !strings.Contains(out.Notes, "10 %") || !strings.Contains(out.Notes, "(in progress)") {
		t.Fatalf("%q", out.Notes)
	}
	if strings.Contains(out.Notes, "recording since") {
		t.Fatalf("history exists: %q", out.Notes)
	}

	// No weekly history yet: say since when it is recorded, never draw it.
	u2 := usageFixture()
	u2.Windows[1].History = nil
	if n := buildQuota(u2, SpanWeeks, KindOutput, false).Notes; !strings.Contains(n, "recording since") {
		t.Fatalf("%q", n)
	}
	u2.RecordingSince = 0
	if n := buildQuota(u2, SpanWeeks, KindOutput, true).Notes; !strings.Contains(n, "no history recorded yet") {
		t.Fatalf("%q", n)
	}
	u2.Weeks = nil
	if v := buildQuota(u2, SpanWeeks, KindOutput, false); len(v.WeekBars.Layers) != 0 {
		t.Fatal("no weeks, no bars")
	}
}

func TestBuildQuotaSaysWhereTheResetCameFromAndWhatFailed(t *testing.T) {
	u := usageFixture()
	u.ResetSource, u.QuotaErr = "recorded", "aiquota not found"
	n := buildQuota(u, SpanWeek, KindOutput, false).Notes
	if !strings.Contains(n, "aiquota: aiquota not found") || !strings.Contains(n, "last recorded reset") {
		t.Fatalf("%q", n)
	}
	u.ResetSource = ""
	if n := buildQuota(u, SpanWeek, KindOutput, false).Notes; !strings.Contains(n, "no weekly reset known") {
		t.Fatalf("%q", n)
	}
	if v := buildQuota(nil, SpanWeek, KindOutput, false); v.Notes == "" || len(v.Out.Layers) != 0 {
		t.Fatal("no usage yet")
	}
}

func TestTokensAndColours(t *testing.T) {
	for in, want := range map[float64]string{0: "0", 999: "999", 1500: "2k", 2.5e6: "2.5M", 3e9: "3.0G"} {
		if got := Tokens(in); got != want {
			t.Errorf("Tokens(%v) = %q, want %q", in, got, want)
		}
	}
	if len(StackColors(false)) != len(model.Families) || len(lineColors(true)) < 5 {
		t.Fatal("palettes")
	}
	seen := map[[3]uint8]bool{}
	for _, c := range StackColors(false) {
		seen[[3]uint8{c.R, c.G, c.B}] = true
	}
	if len(seen) != len(model.Families) {
		t.Fatal("two families share a colour")
	}
	if clock(0) == "" || dayTime(0) == "" || date(0) == "" {
		t.Fatal("time formats")
	}
}

func TestSceneQuotaChartsFollowTheUsageAndTheNotesAreBound(t *testing.T) {
	s := newScene(t)
	s.VM().Apply(withUsage(snap()))
	if s.VM().QuotaNotes.Get() == "" || s.notes.Text().Get() != s.VM().QuotaNotes.Get() {
		t.Fatalf("notes %q / %q", s.VM().QuotaNotes.Get(), s.notes.Text().Get())
	}
	if len(s.weekBars.Layers) != 0 { // the default span is 7 days
		t.Fatal("the weeks chart is fed only when the weeks span is shown")
	}
	s.VM().Span.Set(SpanWeeks)
	if len(s.weekBars.Layers) != 4 || len(s.cum.Overlays) != 4 || s.cum.Max <= 0 {
		t.Fatal("the weeks span feeds the weeks charts")
	}
	s.VM().Span.Set(SpanShort)
	if len(s.official.Overlays) != 1 || s.official.Max != 100 || len(s.outBars.Names) != 4 {
		t.Fatalf("%+v", s.official.Overlays)
	}
	s.VM().Apply(snap()) // a snapshot without usage empties the charts
	if len(s.outBars.Layers) != 0 || s.official.Max != 1 {
		t.Fatal("no usage, no bars")
	}
	if s.cum.Max != 1 {
		t.Fatal("an empty chart still has a scale")
	}
}
