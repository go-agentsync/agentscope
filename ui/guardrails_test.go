package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/toolkit"
)

// guardFixture is a Guardrails view shaped like the real one: twelve weeks, a
// gate that leaks (a recurrence after it landed, one with an inferred year), a
// silent one, a gate that names no hook, and lessons that are not gates.
func guardFixture() *model.Guardrails {
	var weeks []int64
	start := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 12; i++ {
		weeks = append(weeks, start.AddDate(0, 0, 7*i).Unix())
	}
	stated, inferred := make([]float64, 12), make([]float64, 12)
	stated[10], stated[11], inferred[9] = 4, 2, 1
	layer := func(v float64) []float64 {
		l := make([]float64, 12)
		l[10], l[11] = v, v*2
		return l
	}
	return &model.Guardrails{
		At: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC).Unix(), RecurMeasured: true, RefusalsMeasured: true,
		Lessons: 5, Untriaged: 1, Leaking: 1, SilentGates: 1, ThisWeek: 6, LastWeek: 3,
		Weeks: weeks, RecStated: stated, RecInferred: inferred,
		Gates: []string{"guard-bash", "replystamp", "other gate"}, Refusals: [][]float64{layer(2), layer(1), layer(3)},
		Landings: []model.Landing{{At: weeks[10] + 3600, Lesson: "leaky"}, {At: weeks[3], Lesson: "silent"}},
		Rows: []model.GuardLesson{
			{Name: "leaky", Decision: "gate", GateText: "replystamp --pretool\n(PreToolUse) wired 2026-11-13", WrittenAt: time.Date(2026, 11, 13, 17, 56, 0, 0, time.UTC).Unix(),
				Total: 5, Dated: 4, Inferred: 1, Undated: 1, Last: "2026-11-19", After: 2, AfterInferred: 1, Named: []string{"replystamp"},
				Lines: []model.RecLine{{Date: "2026-11-19", Text: "again"}, {Date: "2026-11-18", Inferred: true, Text: "maybe"}, {Text: "no date"}}},
			{Name: "silent", Decision: "gate", GateText: "mutsweep -hook", WrittenAt: time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC).Unix(), Total: 1, Dated: 1, Last: "2026-09-01", Silent: true, Named: []string{"mutsweep"}},
			{Name: "plain", Decision: "gate", GateText: "a test in go-fft/fft", WrittenAt: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC).Unix(), Total: 1, Undated: 1},
			{Name: "todo", Decision: "untriaged", GateText: "seeded, not triaged", Total: 3, Dated: 3, Last: "2026-10-30", Lines: []model.RecLine{{Date: "2026-10-30", Text: "x"}}},
			{Name: "wait", Decision: "pending", Total: 2, Undated: 2},
		},
	}
}

func snapWith(g *model.Guardrails) model.Snapshot {
	sn := snap()
	sn.Guardrails = g
	return sn
}

func names(g *GuardState) []string {
	var out []string
	for _, r := range g.Rows.Slice() {
		out = append(out, r.Name)
	}
	return out
}

func TestGuardRowsAreFilteredByDecisionAndSortedByAnyColumn(t *testing.T) {
	vm := NewViewModel()
	g := vm.Guard
	if g.Rows.Len() != 0 || !strings.Contains(g.Notes.Get(), "reading") {
		t.Fatal("before any data the tab says it is reading")
	}
	vm.Apply(snapWith(guardFixture()))
	// The headline is the gate that leaks: most recurrences after the gate, first.
	if got := names(g); strings.Join(got, ",") != "leaky,plain,silent,todo,wait" {
		t.Fatalf("default order %v", got)
	}
	for _, c := range []struct {
		col  int
		asc  bool
		want string
	}{
		{GColName, true, "leaky,plain,silent,todo,wait"},
		{GColName, false, "wait,todo,silent,plain,leaky"},
		{GColTotal, false, "leaky,todo,wait,plain,silent"}, // 5, 3, 2, then 1 and 1 by name
		{GColTotal, true, "plain,silent,wait,todo,leaky"},
		{GColLast, false, "leaky,todo,silent,plain,wait"}, // plain and wait have none: "" sorts first ascending
		{GColDecision, true, "leaky,plain,silent,wait,todo"},
		{GColGate, true, "wait,plain,silent,leaky,todo"}, // "", "a test", "mutsweep", "replystamp", "seeded"
		{GColAfter, true, "plain,silent,todo,wait,leaky"},
		{GColFlag, false, "silent,leaky,plain,todo,wait"},
	} {
		g.SetSort(c.col, c.asc)
		if got := strings.Join(names(g), ","); got != c.want {
			t.Errorf("column %d ascending=%v: %s, want %s", c.col, c.asc, got, c.want)
		}
	}
	g.SetSort(GColName, true)
	g.Decision.Set(1) // gate
	if got := strings.Join(names(g), ","); got != "leaky,plain,silent" {
		t.Fatalf("gate only: %s", got)
	}
	g.Decision.Set(4) // untriaged
	if got := strings.Join(names(g), ","); got != "todo" {
		t.Fatalf("untriaged only: %s", got)
	}
	g.Decision.Set(99) // not an option: no filter
	if g.Rows.Len() != 5 {
		t.Fatalf("%d rows", g.Rows.Len())
	}
	g.Decision.Set(0)
	if g.Rows.Len() != 5 {
		t.Fatalf("%d rows", g.Rows.Len())
	}
}

func TestGuardRowCellsShowTheGateTruncatedAndTheInferredAndTheFlag(t *testing.T) {
	vm := NewViewModel()
	f := guardFixture()
	f.Rows[0].GateText = strings.Repeat("replystamp --pretool ", 10)
	vm.Apply(snapWith(f))
	var leaky, silent, wait GuardRow
	for _, r := range vm.Guard.Rows.Slice() {
		switch r.Name {
		case "leaky":
			leaky = r
		case "silent":
			silent = r
		case "wait":
			wait = r
		}
	}
	c := leaky.Cells()
	if len(c) != len(GuardTitles()) || c[GColAfter] != "2 (1 inferred)" || c[GColLast] != "2026-11-19" || c[GColFlag] != "" || len(c[GColGate]) > guardGateChars+3 || strings.Contains(c[GColGate], "\n") {
		t.Fatalf("%q", c)
	}
	if c := silent.Cells(); c[GColFlag] != "SILENT" || c[GColAfter] != "0" {
		t.Fatalf("%q", c)
	}
	if c := wait.Cells(); c[GColLast] != "-" {
		t.Fatalf("%q", c)
	}
}

func TestSelectingALessonShowsItsRecurrenceLinesAndTheSelectionFollowsItAcrossAResort(t *testing.T) {
	vm := NewViewModel()
	g := vm.Guard
	vm.Apply(snapWith(guardFixture()))
	if g.Detail.Len() != 0 || g.Sel.Get() != -1 {
		t.Fatal("nothing selected: an empty pane")
	}
	g.Sel.Set(0) // leaky
	joined := strings.Join(g.Detail.Slice(), "\n")
	for _, want := range []string{"leaky", "decision: gate, written 2026-11-13", "recurrences: 5 (4 dated, 1 of them with an inferred year; 1 undated)",
		"after the decision: 2 (1 with an inferred year)", "ledger: replystamp --pretool (PreToolUse) wired 2026-11-13", "recurrence lines:",
		"- 2026-11-19: again", "- 2026-11-18 (year inferred): maybe", "- undated: no date", "... 2 more recurrence line(s)"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the pane lacks %q:\n%s", want, joined)
		}
	}
	g.SetSort(GColName, false)
	if r := g.Rows.At(g.Sel.Get()); r.Name != "leaky" || !strings.Contains(g.Detail.At(0), "leaky") {
		t.Fatalf("the selection left its lesson: %+v", r)
	}
	g.Decision.Set(4) // a filter that drops it
	if g.Sel.Get() != -1 || g.Detail.Len() != 0 {
		t.Fatalf("selection %d, %d lines", g.Sel.Get(), g.Detail.Len())
	}
	g.Decision.Set(0)
	g.Sel.Set(g.Rows.Len() + 3) // out of range: nothing
	if g.Detail.Len() != 0 {
		t.Fatal("a detail for a row that does not exist")
	}
	vm.Apply(snapWith(nil)) // the data goes away
	if g.Rows.Len() != 0 || g.Detail.Len() != 0 {
		t.Fatal("rows of data that is gone")
	}
}

func TestLessonLinesSayWhyASilentGateIsFlaggedAndWhatCannotBeCounted(t *testing.T) {
	lines := func(r model.GuardLesson) string { return strings.Join(lessonLines(r), "\n") }
	if s := lines(model.GuardLesson{Name: "s", Decision: "gate", Silent: true, Named: []string{"mutsweep", "prbody"}}); !strings.Contains(s, "SILENT: landed 4 or more weeks ago, no refusal from mutsweep, prbody") {
		t.Fatal(s)
	}
	if s := lines(model.GuardLesson{Name: "n", Decision: "gate"}); !strings.Contains(s, "refusals of this gate are not counted") {
		t.Fatal(s)
	}
	// A lesson that is not a gate, with no text, no lines and no time says only what it is.
	s := lines(model.GuardLesson{Name: "u", Decision: "untriaged", Total: 2, Undated: 2})
	if strings.Contains(s, "after the decision") || strings.Contains(s, "ledger:") || strings.Contains(s, "recurrence lines") || strings.Contains(s, "SILENT") || strings.Contains(s, "not counted") {
		t.Fatal(s)
	}
	if s := lines(model.GuardLesson{Name: "w", Decision: "gate", WrittenAt: 1, After: 1, Named: []string{"x"}}); !strings.Contains(s, "after the decision: 1 (dated after the day it was written)") {
		t.Fatal(s)
	}
}

func TestTheDetailPaneIsBoundedAndWrapsAtTheWidthItIsGiven(t *testing.T) {
	vm := NewViewModel()
	f := guardFixture()
	for i := 0; i < 300; i++ {
		f.Rows[0].Lines = append(f.Rows[0].Lines, model.RecLine{Date: "2026-11-01", Text: fmt.Sprintf("line %d", i)})
	}
	f.Rows[0].GateText = strings.Repeat("word ", 80)
	vm.Apply(snapWith(f))
	vm.Guard.Sel.Set(0)
	n := vm.Guard.Detail.Len()
	if n != maxDetailLines+1 || !strings.Contains(vm.Guard.Detail.At(n-1), "more line(s)") {
		t.Fatalf("%d lines", n)
	}
}

// The Selected-lesson pane breaks its lines at the width it is given: the widget
// does it, so the lines at a narrow pane are more and none is wider than the pane.
func TestTheLessonPaneWrapsAtItsWidth(t *testing.T) {
	s := newScene(t)
	f := guardFixture()
	f.Rows[0].GateText = strings.Repeat("word ", 80)
	s.VM().Apply(snapWith(f))
	s.VM().Tab.Set(TabGuardrails)
	s.VM().Guard.Sel.Set(0)
	s.Resize(testW, testH, 1)
	wide := len(s.gDetail.Lines())
	s.Resize(testW/2, testH, 1)
	narrow := s.gDetail.Lines()
	if len(narrow) <= wide {
		t.Fatalf("%d lines narrow, %d wide", len(narrow), wide)
	}
	room := s.gDetail.Bounds().W
	for _, l := range narrow {
		if w := toolkit.TextWidth(l); w > room {
			t.Fatalf("a line %d px wide in a pane of %d: %q", w, room, l)
		}
	}
}

func TestGuardChipsAndNotesSayNotMeasuredNeverZero(t *testing.T) {
	if c := guardChips(nil); strings.Contains(strings.Join(c, "|"), "0") || len(c) != GuardChipSlots {
		t.Fatalf("%q", c)
	}
	if n := guardNotes(nil); !strings.Contains(n, "reading") {
		t.Fatal(n)
	}
	ok := guardFixture()
	if c := guardChips(ok); strings.Join(c, "|") != "untriaged 1|leaking gates 1|silent gates 1|refusals 6 this week, 3 last" {
		t.Fatalf("%q", c)
	}
	n := guardNotes(ok)
	if !strings.Contains(n, "5 lessons; 4 undated recurrences are not on the chart; 1 dated ones have an inferred year") {
		t.Fatal(n)
	}
	if strings.Contains(n, "not measured") || strings.Contains(n, "unreadable") {
		t.Fatal(n)
	}

	noRecur := guardFixture()
	noRecur.RecurMeasured, noRecur.RecurErr = false, "recur not found"
	c := guardChips(noRecur)
	if c[0] != "untriaged: not measured" || c[1] != "leaking gates: not measured" || c[2] != "silent gates: not measured" || !strings.HasPrefix(c[3], "refusals 6") {
		t.Fatalf("%q", c)
	}
	if n := guardNotes(noRecur); !strings.Contains(n, "recur not measured: recur not found") {
		t.Fatal(n)
	}

	noRefusals := guardFixture()
	noRefusals.RefusalsMeasured = false
	noRefusals.Unreadable = 2
	c = guardChips(noRefusals)
	if c[0] != "untriaged 1" || c[2] != "silent gates: not measured" || c[3] != "refusals: not measured" {
		t.Fatalf("%q", c)
	}
	if n := guardNotes(noRefusals); !strings.Contains(n, "refusals not measured") || !strings.Contains(n, "2 memory file(s) unreadable") {
		t.Fatal(n)
	}
}

func TestBuildGuardDrawsWhatWasMeasuredAndKeepsInferredYearsApart(t *testing.T) {
	if v := buildGuard(nil, false); v.Rec.Overlays != nil || v.Bars.Layers != nil {
		t.Fatal("a chart without data")
	}
	if v := buildGuard(&model.Guardrails{}, false); v.Rec.Overlays != nil {
		t.Fatal("a chart without weeks")
	}
	g := guardFixture()
	v := buildGuard(g, false)
	if len(v.Rec.Overlays) != 2 || v.Rec.Overlays[0].Name != "recurrences (dated)" || v.Rec.Overlays[1].Name != "year inferred" {
		t.Fatalf("%+v", v.Rec.Overlays)
	}
	if v.Rec.Overlays[0].Ink == v.Rec.Overlays[1].Ink {
		t.Fatal("stated and inferred are drawn alike")
	}
	st, inf := v.Rec.Overlays[0].Points, v.Rec.Overlays[1].Points
	if len(st) != 12 || st[10].Value != 4 || st[11].Value != 2 || inf[9].Value != 1 || inf[10].Value != 0 {
		t.Fatalf("%v %v", st, inf)
	}
	// A point sits in the middle of its week, so it lines up with its bar.
	if st[0].At != g.Weeks[0]+3*24*3600+12*3600 {
		t.Fatalf("at %d, week starts %d", st[0].At, g.Weeks[0])
	}
	if v.Rec.TimeMin != g.Weeks[0] || v.Rec.TimeMax != g.Weeks[11]+7*24*3600 || v.Bars.TimeMin != v.Rec.TimeMin || v.Bars.TimeMax != v.Rec.TimeMax {
		t.Fatal("the two charts do not share a time axis")
	}
	if v.Rec.Max != toolkit.NiceCeiling(4) || len(v.Rec.Markers) != 2 || v.Rec.Markers[0].At != g.Landings[0].At {
		t.Fatalf("%v %v", v.Rec.Max, v.Rec.Markers)
	}
	if got := v.Rec.FormatTime(g.Weeks[0]); got != "Aug 31" {
		t.Fatal(got)
	}
	if len(v.Bars.Layers) != 3 || len(v.Names) != 3 || len(v.Colors) != 3 {
		t.Fatalf("%d layers, %d names, %d colours", len(v.Bars.Layers), len(v.Names), len(v.Colors))
	}
	// More gates than colours: the palette cycles, no layer is left without one.
	many := guardFixture()
	for i := 0; i < 10; i++ {
		many.Gates = append(many.Gates, fmt.Sprintf("g%d", i))
		many.Refusals = append(many.Refusals, make([]float64, 12))
	}
	if v := buildGuard(many, true); len(v.Colors) != len(many.Gates) {
		t.Fatalf("%d colours for %d gates", len(v.Colors), len(many.Gates))
	}
	// A peak of nothing still gives the axis a scale.
	flat := guardFixture()
	flat.RecStated, flat.RecInferred = make([]float64, 12), make([]float64, 12)
	if v := buildGuard(flat, false); v.Rec.Max <= 0 {
		t.Fatalf("%v", v.Rec.Max)
	}

	noRecur := guardFixture()
	noRecur.RecurMeasured = false
	if v := buildGuard(noRecur, false); v.Rec.Overlays != nil || v.Rec.Markers != nil || len(v.Bars.Layers) != 3 {
		t.Fatal("recur not measured: no curve, but the refusals are")
	}
	noRef := guardFixture()
	noRef.RefusalsMeasured = false
	if v := buildGuard(noRef, false); v.Bars.Layers != nil || v.Names != nil || len(v.Rec.Overlays) != 2 {
		t.Fatal("refusals not measured: no bars, but the curve is")
	}
}
