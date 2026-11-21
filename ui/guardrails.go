package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/mvvm"
	"github.com/go-widgets/toolkit"
)

// The Guardrails tab's decision filter: DecisionNames[0] is every decision, the
// others are the decisions the ledger records.
var DecisionNames = []string{"All decisions", "gate", "pending", "no-gate", "untriaged", "none"}

// GuardTitles are the lessons table's column titles; the columns are sortable.
var GuardTitles = []string{"Lesson", "Recur.", "Last", "Decision", "Gate", "After gate", "Flag"}

// The columns of the lessons table.
const (
	GColName = iota
	GColTotal
	GColLast
	GColDecision
	GColGate
	GColAfter
	GColFlag
)

// guardGateChars is how much of a gate's text the table shows.
const guardGateChars = 60

// GuardChipSlots is how many headline chips the tab shows.
const GuardChipSlots = 4

// GuardRow is one lesson of the table.
type GuardRow struct {
	Name     string
	Total    int
	Last     string
	Decision string
	Gate     string // truncated text
	After    int
	// AfterInferred of After have a year `recur` inferred.
	AfterInferred int
	Silent        bool
}

// Cells are the row as the table shows it.
func (r GuardRow) Cells() []string {
	after := strconv.Itoa(r.After)
	if r.AfterInferred > 0 {
		after += " (" + strconv.Itoa(r.AfterInferred) + " inferred)"
	}
	flag := ""
	if r.Silent {
		flag = "SILENT"
	}
	last := r.Last
	if last == "" {
		last = "-"
	}
	return []string{r.Name, strconv.Itoa(r.Total), last, r.Decision, r.Gate, after, flag}
}

// GuardState is the Guardrails tab's state: the data, the table (filtered by
// decision and sorted), the selection and what it shows, and the headline chips.
type GuardState struct {
	Data *mvvm.Observable[*model.Guardrails]
	Rows *mvvm.ObservableList[GuardRow]
	// Sel is the selected table row, Detail the pane showing that lesson.
	Sel    *mvvm.Observable[int]
	Detail *mvvm.ObservableList[string]
	// Decision indexes DecisionNames; SortCol and SortAsc order the table.
	Decision *mvvm.Observable[int]
	SortCol  *mvvm.Observable[int]
	SortAsc  *mvvm.Observable[bool]
	// Chips are the headline chips; Notes the line under the tab, which says what
	// is not measured.
	Chips [GuardChipSlots]*mvvm.Observable[string]
	Notes *mvvm.Observable[string]
	rows  []GuardRow // every row, before the filter
}

func newGuardState() *GuardState {
	g := &GuardState{
		Data: mvvm.NewObservable[*model.Guardrails](nil), Rows: mvvm.NewObservableList[GuardRow](),
		Sel: mvvm.NewObservable(-1), Detail: mvvm.NewObservableList[string](),
		Decision: mvvm.NewObservable(0), SortCol: mvvm.NewObservable(GColAfter), SortAsc: mvvm.NewObservable(false),
		Notes: mvvm.NewObservable("reading the ledger and the transcripts..."),
	}
	for i := range g.Chips {
		g.Chips[i] = mvvm.NewObservable("")
	}
	g.Data.SubscribeChanged(g.onData)
	g.Decision.SubscribeChanged(g.refreshRows)
	g.SortCol.SubscribeChanged(g.refreshRows)
	g.SortAsc.SubscribeChanged(g.refreshRows)
	g.Sel.SubscribeChanged(g.refreshDetail)
	return g
}

// SetSort is what a header click does: it orders the table by col.
func (g *GuardState) SetSort(col int, asc bool) {
	g.SortCol.Set(col)
	g.SortAsc.Set(asc)
}

// onData rebuilds the rows, the chips and the notes from the data.
func (g *GuardState) onData() {
	d := g.Data.Get()
	g.rows = nil
	if d != nil {
		for _, r := range d.Rows {
			g.rows = append(g.rows, GuardRow{Name: r.Name, Total: r.Total, Last: r.Last, Decision: r.Decision,
				Gate: model.Truncate(oneLine(r.GateText), guardGateChars), After: r.After, AfterInferred: r.AfterInferred, Silent: r.Silent})
		}
	}
	for i, c := range guardChips(d) {
		g.Chips[i].Set(c)
	}
	g.Notes.Set(guardNotes(d))
	g.refreshRows()
}

// oneLine joins a text's lines: a table cell is one line.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// refreshRows applies the decision filter and the sort to the rows, and keeps the
// selection on the same lesson.
func (g *GuardState) refreshRows() {
	keep := ""
	if i := g.Sel.Get(); i >= 0 && i < g.Rows.Len() {
		keep = g.Rows.At(i).Name
	}
	want := ""
	if d := g.Decision.Get(); d > 0 && d < len(DecisionNames) {
		want = DecisionNames[d]
	}
	var rows []GuardRow
	for _, r := range g.rows {
		if want == "" || r.Decision == want {
			rows = append(rows, r)
		}
	}
	col, asc := g.SortCol.Get(), g.SortAsc.Get()
	sort.SliceStable(rows, func(i, j int) bool {
		if c := compareRows(rows[i], rows[j], col); c != 0 {
			return (c < 0) == asc
		}
		return rows[i].Name < rows[j].Name
	})
	setList(g.Rows, rows)
	idx := -1
	for i, r := range rows {
		if r.Name == keep {
			idx = i
		}
	}
	g.Sel.Set(idx)
	g.refreshDetail()
}

// compareRows orders two rows by a column: numbers as numbers, the rest as text.
func compareRows(a, b GuardRow, col int) int {
	cmpInt := func(x, y int) int {
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	}
	switch col {
	case GColTotal:
		return cmpInt(a.Total, b.Total)
	case GColAfter:
		return cmpInt(a.After, b.After)
	case GColFlag:
		return cmpInt(boolInt(a.Silent), boolInt(b.Silent))
	case GColLast:
		return strings.Compare(a.Last, b.Last)
	case GColDecision:
		return strings.Compare(a.Decision, b.Decision)
	case GColGate:
		return strings.Compare(a.Gate, b.Gate)
	}
	return strings.Compare(a.Name, b.Name)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// refreshDetail writes the pane for the selected lesson.
func (g *GuardState) refreshDetail() {
	d, i := g.Data.Get(), g.Sel.Get()
	var lines []string
	if d != nil && i >= 0 && i < g.Rows.Len() {
		for _, r := range d.Rows {
			if r.Name == g.Rows.At(i).Name {
				lines = lessonLines(r)
			}
		}
	}
	setList(g.Detail, capLines(lines))
}

// lessonLines describes one lesson: its decision, what its recurrences say about
// the gate, and the recurrence lines themselves.
func lessonLines(r model.GuardLesson) []string {
	lines := []string{r.Name, ""}
	dec := "decision: " + r.Decision
	if r.WrittenAt > 0 {
		dec += ", written " + time.Unix(r.WrittenAt, 0).Format("2006-01-02 15:04")
	}
	lines = append(lines, dec)
	lines = append(lines, fmt.Sprintf("recurrences: %d (%d dated, %d of them with an inferred year; %d undated)", r.Total, r.Dated, r.Inferred, r.Undated))
	if r.WrittenAt > 0 {
		s := fmt.Sprintf("after the decision: %d", r.After)
		if r.AfterInferred > 0 {
			s += fmt.Sprintf(" (%d with an inferred year)", r.AfterInferred)
		}
		lines = append(lines, s+" (dated after the day it was written)")
	}
	if r.Silent {
		lines = append(lines, "SILENT: landed "+strconv.Itoa(silentWeeks)+" or more weeks ago, no refusal from "+strings.Join(r.Named, ", ")+" in that time, and no recurrence since: either obsolete or no longer running")
	} else if r.Decision == "gate" && len(r.Named) == 0 {
		lines = append(lines, "refusals of this gate are not counted: its text names no hook the transcripts show")
	}
	if r.GateText != "" {
		lines = append(lines, "", "ledger: "+oneLine(r.GateText))
	}
	if len(r.Lines) > 0 {
		lines = append(lines, "", "recurrence lines:")
	}
	for _, l := range r.Lines {
		d := "undated"
		switch {
		case l.Date != "" && l.Inferred:
			d = l.Date + " (year inferred)"
		case l.Date != "":
			d = l.Date
		}
		lines = append(lines, "- "+d+": "+oneLine(l.Text))
	}
	if r.Total > len(r.Lines) {
		lines = append(lines, "... "+strconv.Itoa(r.Total-len(r.Lines))+" more recurrence line(s)")
	}
	return lines
}

// silentWeeks mirrors guard.SilentWeeks for the pane's text.
const silentWeeks = 4

// guardChips are the headline chips: untriaged lessons, leaking gates, silent
// gates, and the refusals of this week against last. A source that was not
// measured says so; it is never 0.
func guardChips(d *model.Guardrails) []string {
	out := []string{"untriaged: ...", "leaking gates: ...", "silent gates: ...", "refusals: ..."}
	if d == nil {
		return out
	}
	if d.RecurMeasured {
		out[0] = "untriaged " + strconv.Itoa(d.Untriaged)
		out[1] = "leaking gates " + strconv.Itoa(d.Leaking)
	} else {
		out[0], out[1] = "untriaged: not measured", "leaking gates: not measured"
	}
	if d.RecurMeasured && d.RefusalsMeasured {
		out[2] = "silent gates " + strconv.Itoa(d.SilentGates)
	} else {
		out[2] = "silent gates: not measured"
	}
	if d.RefusalsMeasured {
		out[3] = fmt.Sprintf("refusals %d this week, %d last", d.ThisWeek, d.LastWeek)
	} else {
		out[3] = "refusals: not measured"
	}
	return out
}

// guardNotes is the line under the tab: what is not measured, and what the charts
// leave out.
func guardNotes(d *model.Guardrails) string {
	if d == nil {
		return "reading the ledger and the transcripts..."
	}
	var n []string
	if !d.RecurMeasured {
		n = append(n, "recur not measured: "+d.RecurErr)
	} else {
		undated, inferred := 0, 0
		for _, r := range d.Rows {
			undated += r.Undated
			inferred += r.Inferred
		}
		n = append(n, fmt.Sprintf("%d lessons; %d undated recurrences are not on the chart; %d dated ones have an inferred year (drawn as their own line)", d.Lessons, undated, inferred))
		if d.Unreadable > 0 {
			n = append(n, strconv.Itoa(d.Unreadable)+" memory file(s) unreadable")
		}
	}
	if !d.RefusalsMeasured {
		n = append(n, "refusals not measured: the transcripts have not been scanned")
	}
	return strings.Join(n, "  |  ")
}

// guardView is the Guardrails tab's two charts.
type guardView struct {
	Rec   lineData // recurrences per week, and the gate landings
	Bars  barData  // refusals per week, stacked by gate
	Names []string
	// Colors are the stacked layers' colours.
	Colors []toolkit.RGBA
}

// buildGuard turns the data into charts. Weeks are drawn at the middle of their
// bar, so the two charts line up; what was not measured is left empty.
func buildGuard(d *model.Guardrails, dark bool) guardView {
	var v guardView
	if d == nil || len(d.Weeks) == 0 {
		return v
	}
	first := d.Weeks[0]
	end := time.Unix(d.Weeks[len(d.Weeks)-1], 0).AddDate(0, 0, 7).Unix()
	short := func(at int64) string { return time.Unix(at, 0).Format("Jan 2") }
	v.Rec = lineData{TimeMin: first, TimeMax: end, FormatTime: short}
	v.Bars = barData{TimeMin: first, TimeMax: end, FormatTime: short}
	p := Palette(dark)
	if d.RefusalsMeasured {
		v.Bars.Layers, v.Names = d.Refusals, d.Gates
		for i := range d.Gates {
			v.Colors = append(v.Colors, p[i%len(p)])
		}
	}
	if !d.RecurMeasured {
		return v
	}
	var stated, inferred []toolkit.TimePoint
	var peak float64
	for i, w := range d.Weeks {
		mid := w + (end-first)/int64(len(d.Weeks))/2
		stated = append(stated, toolkit.TimePoint{At: mid, Value: d.RecStated[i]})
		inferred = append(inferred, toolkit.TimePoint{At: mid, Value: d.RecInferred[i]})
		peak = max(peak, d.RecStated[i], d.RecInferred[i])
	}
	v.Rec.Overlays = []toolkit.TimeLine{
		{Name: "recurrences (dated)", Ink: Brand, Points: stated},
		{Name: "year inferred", Ink: p[5], Points: inferred},
	}
	v.Rec.Max = toolkit.NiceCeiling(max(peak, 1))
	for _, l := range d.Landings {
		v.Rec.Markers = append(v.Rec.Markers, toolkit.TimeMarker{At: l.At, Ink: p[2]})
	}
	return v
}
