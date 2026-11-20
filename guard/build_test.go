package guard

import (
	"strings"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
)

// A Friday, so the current week starts on Monday 2026-11-16 and the twelve weeks
// on Monday 2026-08-31.
var friday = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

func sp(s string) *string { return &s }

// lesson builds a lesson of the recur JSON: decision, ledger text, writtenAt and
// the recurrences as "date" or "date~" (year inferred) or "" (undated).
func lesson(name, decision, text string, writtenAt *string, recs ...string) Lesson {
	var l Lesson
	l.Name = name
	l.Ledger.Decision, l.Ledger.Text, l.Ledger.WrittenAt = decision, text, writtenAt
	for _, r := range recs {
		var rec struct {
			Date         *string `json:"date"`
			YearInferred bool    `json:"yearInferred"`
			Line         string  `json:"line"`
		}
		rec.Line = "line " + r
		switch {
		case r == "":
		case strings.HasSuffix(r, "~"):
			rec.Date, rec.YearInferred = sp(strings.TrimSuffix(r, "~")), true
		default:
			rec.Date = sp(r)
		}
		l.Recurrences = append(l.Recurrences, rec)
	}
	return l
}

func recurrence(ls ...Lesson) Recurrence {
	r := Recurrence{Lessons: ls}
	r.Totals.Lessons = len(ls)
	return r
}

func row(g *model.Guardrails, name string) model.GuardLesson {
	for _, r := range g.Rows {
		if r.Name == name {
			return r
		}
	}
	return model.GuardLesson{}
}

func TestTheWeeksAreTwelveMondaysEndingWithThisOne(t *testing.T) {
	g := Build(Input{Now: friday})
	if len(g.Weeks) != Weeks {
		t.Fatalf("%d weeks", len(g.Weeks))
	}
	first, last := time.Unix(g.Weeks[0], 0).UTC(), time.Unix(g.Weeks[Weeks-1], 0).UTC()
	if first.Format(time.DateOnly) != "2026-08-31" || last.Format(time.DateOnly) != "2026-11-16" || last.Weekday() != time.Monday {
		t.Fatalf("%s .. %s", first, last)
	}
	// Sunday evening and Monday morning are in different weeks.
	if weekStart(time.Date(2026, 11, 22, 23, 0, 0, 0, time.UTC)).Day() != 16 || weekStart(time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)).Day() != 23 {
		t.Fatal("a week does not run Monday to Sunday")
	}
}

func TestNothingMeasuredIsNotZero(t *testing.T) {
	g := Build(Input{Now: friday, RecErr: "recur not found"})
	if g.RecurMeasured || g.RefusalsMeasured || g.RecurErr != "recur not found" || len(g.Rows) != 0 {
		t.Fatalf("%+v", g)
	}
	// The unmeasured source leaves its series empty-handed; the shape stays.
	if len(g.Weeks) != Weeks || len(g.RecStated) != Weeks || g.Gates != nil {
		t.Fatalf("%+v", g)
	}
	// recur read but the transcripts not scanned: each says what it is.
	g = Build(Input{Now: friday, Rec: recurrence(lesson("a", "untriaged", "seeded", nil, "2026-11-10"))})
	if !g.RecurMeasured || g.RefusalsMeasured || g.Untriaged != 1 {
		t.Fatalf("%+v", g)
	}
}

func TestARecurrenceAfterTheGateCountsOnTheLaterDaySide(t *testing.T) {
	// Written Thursday 2026-11-19 at 21:30 UTC.
	w := sp("2026-11-19T21:30:00Z")
	g := Build(Input{Now: friday, Rec: recurrence(
		lesson("before", "gate", "replystamp", w, "2026-11-12", "2026-11-18"), // both before the day: 0 after
		lesson("same-day", "gate", "replystamp", w, "2026-11-19"),             // the day it was written: not after
		lesson("after", "gate", "replystamp", w, "2026-11-12", "2026-11-20", "2026-11-21"),
		lesson("undated-only", "gate", "replystamp", w, ""), // an undated one cannot be placed: not counted
		lesson("no-decision-time", "untriaged", "seeded", nil, "2026-11-20"),
	)})
	for name, want := range map[string]int{"before": 0, "same-day": 0, "after": 2, "undated-only": 0, "no-decision-time": 0} {
		if got := row(g, name).After; got != want {
			t.Errorf("%s: %d recurrence(s) after the gate, want %d", name, got, want)
		}
	}
	if g.Leaking != 1 {
		t.Fatalf("%d leaking gates", g.Leaking)
	}
	if r := row(g, "undated-only"); r.Undated != 1 || r.Dated != 0 || r.Total != 1 || r.Last != "" {
		t.Fatalf("%+v", r)
	}
}

func TestTheDayABoundaryFallsOnFollowsTheLocalZone(t *testing.T) {
	// 23:30 UTC on the 19th is the 20th in Paris (UTC+1): a recurrence dated the 20th
	// is the same day there, and the 21st is after it.
	paris := time.FixedZone("CET", 3600)
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, paris)
	w := sp("2026-11-19T23:30:00Z")
	g := Build(Input{Now: now, Rec: recurrence(lesson("l", "gate", "x", w, "2026-11-20", "2026-11-21"))})
	if got := row(g, "l").After; got != 1 {
		t.Fatalf("%d after", got)
	}
}

func TestAnInferredYearIsKeptApartInTheCountsAndTheSeries(t *testing.T) {
	w := sp("2026-11-01T10:00:00Z")
	g := Build(Input{Now: friday, Rec: recurrence(
		lesson("l", "gate", "x", w,
			"2026-11-10", "2026-11-10", // stated, week 11 (Mon 2026-11-09)
			"2026-11-11~", // inferred, same week
			"2026-11-18~", // inferred, this week, after the gate
			"2025-12-11~", // inferred and long before the twelve weeks
			"2026-11-25"), // in the future: counted as a recurrence, not charted
	)})
	r := row(g, "l")
	if r.Total != 6 || r.Dated != 6 || r.Inferred != 3 || r.After != 5 || r.AfterInferred != 2 {
		t.Fatalf("%+v", r)
	}
	if r.Last != "2026-11-25" {
		t.Fatalf("last %q", r.Last)
	}
	w11, w12 := Weeks-2, Weeks-1
	if g.RecStated[w11] != 2 || g.RecInferred[w11] != 1 || g.RecInferred[w12] != 1 || g.RecStated[w12] != 0 {
		t.Fatalf("stated %v inferred %v", g.RecStated, g.RecInferred)
	}
	sum := func(a []float64) (n float64) {
		for _, v := range a {
			n += v
		}
		return
	}
	if sum(g.RecStated) != 2 || sum(g.RecInferred) != 2 {
		t.Fatalf("a recurrence outside the shown weeks was drawn: %v %v", g.RecStated, g.RecInferred)
	}
}

func TestRefusalsAreStackedPerWeekByGateAndOtherGateIsKept(t *testing.T) {
	h := func(s string) int64 {
		tm, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return tm.Unix()
	}
	g := Build(Input{Now: friday, Scanned: true, Refusals: []Count{
		{h("2026-11-16T00:00:00Z"), GuardBash, 5}, // Monday 00:00: this week
		{h("2026-11-15T23:00:00Z"), GuardBash, 3}, // Sunday 23:00: last week
		{h("2026-11-17T10:00:00Z"), Other, 2},
		{h("2026-11-17T10:00:00Z"), Replystamp, 4},
		{h("2026-11-10T10:00:00Z"), "a gate this build never heard of", 1},
		{h("2026-08-30T23:00:00Z"), GuardBash, 100}, // before the twelve weeks
		{h("2026-11-23T00:00:00Z"), GuardBash, 100}, // next week
		{h("2026-11-17T10:00:00Z"), Modtidy, 0},     // nothing
	}})
	if !g.RefusalsMeasured {
		t.Fatal("scanned, but not measured")
	}
	want := []string{GuardBash, Replystamp, Other, "a gate this build never heard of"}
	if strings.Join(g.Gates, "|") != strings.Join(want, "|") {
		t.Fatalf("%v", g.Gates)
	}
	if g.ThisWeek != 11 || g.LastWeek != 4 {
		t.Fatalf("this week %d, last week %d", g.ThisWeek, g.LastWeek)
	}
	if g.Refusals[0][Weeks-1] != 5 || g.Refusals[0][Weeks-2] != 3 || g.Refusals[0][0] != 0 {
		t.Fatalf("%v", g.Refusals[0])
	}
}

func TestASilentGateLandedLongAgoWithNoRefusalAndNoRecurrence(t *testing.T) {
	old := sp("2026-10-01T10:00:00Z") // seven weeks before
	young := sp("2026-11-10T10:00:00Z")
	recs := recurrence(
		lesson("silent", "gate", "replystamp --pretool", old, "2026-09-01"),
		lesson("busy", "gate", "mutsweep -hook", old),
		lesson("leaking", "gate", "modtidy", old, "2026-11-02"),
		lesson("young", "gate", "ctxgate", young),
		lesson("a-test", "gate", "TestX in go-fft/fft, a CI step", old),
		lesson("two", "gate", "modelgate and prbody", old),
		lesson("pending", "pending", "replystamp", old),
		lesson("no-time", "gate", "replystamp", nil),
	)
	at := friday.Add(-48 * time.Hour).Unix()
	refs := []Count{{at, Mutsweep, 3}, {at, Prbody, 1}, {friday.Add(-60 * 24 * time.Hour).Unix(), Replystamp, 9}} // replystamp refused, but not in the last 4 weeks
	g := Build(Input{Now: friday, Rec: recs, Refusals: refs, Scanned: true})
	for name, want := range map[string]bool{"silent": true, "busy": false, "leaking": false, "young": false, "a-test": false, "two": false, "pending": false, "no-time": false} {
		if got := row(g, name).Silent; got != want {
			t.Errorf("%s: silent %v, want %v", name, got, want)
		}
	}
	if g.SilentGates != 1 || g.Leaking != 1 {
		t.Fatalf("silent %d leaking %d", g.SilentGates, g.Leaking)
	}
	if got := row(g, "silent").Named; len(got) != 1 || got[0] != Replystamp {
		t.Fatalf("%v", got)
	}
	// Without a scan of the transcripts nothing is known about refusals: no flag.
	g = Build(Input{Now: friday, Rec: recs})
	if g.SilentGates != 0 || row(g, "silent").Silent {
		t.Fatal("a gate was called silent without any refusal data")
	}
}

func TestLandingsAreTheGateDecisionsWithATimeOldestFirst(t *testing.T) {
	g := Build(Input{Now: friday, Rec: recurrence(
		lesson("b", "gate", "x", sp("2026-11-12T10:00:00Z")),
		lesson("a", "gate", "x", sp("2026-10-12T10:00:00Z")),
		lesson("c", "pending", "x", sp("2026-11-01T10:00:00Z")),
		lesson("d", "gate", "x", nil),
		lesson("e", "gate", "x", sp("not a time")),
	)})
	if len(g.Landings) != 2 || g.Landings[0].Lesson != "a" || g.Landings[1].Lesson != "b" {
		t.Fatalf("%+v", g.Landings)
	}
	if row(g, "e").WrittenAt != 0 || row(g, "e").After != 0 {
		t.Fatalf("%+v", row(g, "e"))
	}
}

func TestTextsAreBoundedAndNeverCutInsideACharacter(t *testing.T) {
	long := "a" + strings.Repeat("é", maxGateText) // two bytes each: byte 600 is inside one
	var recs []string
	for i := 0; i < maxLines+5; i++ {
		recs = append(recs, "2026-11-10")
	}
	g := Build(Input{Now: friday, Rec: recurrence(lesson("l", "gate", long, nil, recs...))})
	r := row(g, "l")
	if len(r.GateText) > maxGateText+3 || !strings.HasSuffix(r.GateText, "...") || strings.ContainsRune(strings.TrimSuffix(r.GateText, "..."), '�') {
		t.Fatalf("%d bytes", len(r.GateText))
	}
	if len(r.Lines) != maxLines || r.Total != maxLines+5 || r.Dated != maxLines+5 {
		t.Fatalf("%d lines, total %d", len(r.Lines), r.Total)
	}
	if cut("short", 10) != "short" || cut("exact", 5) != "exact" || cut("exactly", 5) != "exact..." {
		t.Fatal("a text was cut at the wrong length")
	}
	if got := cut("aé", 2); got != "a..." { // the cut falls inside é
		t.Fatalf("%q", got)
	}
}

func TestUnreadableMemoriesAreCarriedAlong(t *testing.T) {
	r := recurrence(lesson("l", "untriaged", "seeded", nil))
	r.Totals.Unreadable = 3
	if g := Build(Input{Now: friday, Rec: r}); g.Unreadable != 3 || g.Lessons != 1 {
		t.Fatalf("%+v", g)
	}
}
