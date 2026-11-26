package ui

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/mvvm"
	"github.com/go-widgets/toolkit"
)

// The Guardrails tab's decision filter: the first option is every decision, the
// others are the decisions the ledger records, whose names are the ledger's own
// words (decisionTokens[0] is unused).
var decisionTokens = []string{"", "gate", "pending", "no-gate", "untriaged", "none"}

// DecisionNames are the filter's options: "All decisions" in the language in use,
// then the ledger's words.
func DecisionNames() []string {
	return append([]string{T("dec.all")}, decisionTokens[1:]...)
}

// GuardTitles are the lessons table's column titles, in the language in use; the
// columns are sortable.
func GuardTitles() []string {
	return []string{T("gcol.lesson"), T("gcol.recur"), T("gcol.last"), T("gcol.decision"), T("gcol.gate"), T("gcol.after"), T("gcol.flag")}
}

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
		after += Tf("gd.inferred", r.AfterInferred)
	}
	flag := ""
	if r.Silent {
		flag = T("gd.silent")
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
	// DecisionOpts are the decision drop-down's options, in the language in use.
	DecisionOpts *mvvm.ObservableList[string]
	SortCol      *mvvm.Observable[int]
	SortAsc      *mvvm.Observable[bool]
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
		Decision: mvvm.NewObservable(0), DecisionOpts: mvvm.NewObservableList(DecisionNames()...), SortCol: mvvm.NewObservable(GColAfter), SortAsc: mvvm.NewObservable(false),
		Notes: mvvm.NewObservable(T("gd.reading")),
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
	if d := g.Decision.Get(); d > 0 && d < len(decisionTokens) {
		want = decisionTokens[d]
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
	dec := Tf("gd.decision", r.Decision)
	if r.WrittenAt > 0 {
		dec += Tf("gd.written", time.Unix(r.WrittenAt, 0).Format("2006-01-02 15:04"))
	}
	lines = append(lines, dec)
	lines = append(lines, Tf("gd.recurrences", r.Total, r.Dated, r.Inferred, r.Undated))
	if r.WrittenAt > 0 {
		s := Tf("gd.after", r.After)
		if r.AfterInferred > 0 {
			s += Tf("gd.afterInferred", r.AfterInferred)
		}
		lines = append(lines, s+T("gd.afterDated"))
	}
	if r.Silent {
		lines = append(lines, Tf("gd.silentWhy", silentWeeks, strings.Join(r.Named, ", ")))
	} else if r.Decision == "gate" && len(r.Named) == 0 {
		lines = append(lines, T("gd.uncounted"))
	}
	if r.GateText != "" {
		lines = append(lines, "", Tf("gd.ledger", oneLine(r.GateText)))
	}
	if len(r.Lines) > 0 {
		lines = append(lines, "", T("gd.recLines"))
	}
	for _, l := range r.Lines {
		d := T("gd.undated")
		switch {
		case l.Date != "" && l.Inferred:
			d = l.Date + T("gd.yearInferred")
		case l.Date != "":
			d = l.Date
		}
		lines = append(lines, "- "+d+": "+oneLine(l.Text))
	}
	if r.Total > len(r.Lines) {
		lines = append(lines, Tf("gd.moreRec", r.Total-len(r.Lines)))
	}
	return lines
}

// silentWeeks mirrors guard.SilentWeeks for the pane's text.
const silentWeeks = 4

// guardChips are the headline chips: untriaged lessons, leaking gates, silent
// gates, and the refusals of this week against last. A source that was not
// measured says so; it is never 0.
func guardChips(d *model.Guardrails) []string {
	out := []string{T("gd.c.untriagedWait"), T("gd.c.leakingWait"), T("gd.c.silentWait"), T("gd.c.refusalsWait")}
	if d == nil {
		return out
	}
	if d.RecurMeasured {
		out[0] = Tf("gd.c.untriaged", d.Untriaged)
		out[1] = Tf("gd.c.leaking", d.Leaking)
	} else {
		out[0], out[1] = T("gd.c.untriagedNo"), T("gd.c.leakingNo")
	}
	if d.RecurMeasured && d.RefusalsMeasured {
		out[2] = Tf("gd.c.silent", d.SilentGates)
	} else {
		out[2] = T("gd.c.silentNo")
	}
	if d.RefusalsMeasured {
		out[3] = Tf("gd.c.refusals", d.ThisWeek, d.LastWeek)
	} else {
		out[3] = T("gd.c.refusalsNo")
	}
	return out
}

// guardNotes is the line under the tab: what is not measured, and what the charts
// leave out.
func guardNotes(d *model.Guardrails) string {
	if d == nil {
		return T("gd.reading")
	}
	var n []string
	if !d.RecurMeasured {
		n = append(n, Tf("gd.n.recurNo", d.RecurErr))
	} else {
		undated, inferred := 0, 0
		for _, r := range d.Rows {
			undated += r.Undated
			inferred += r.Inferred
		}
		n = append(n, Tf("gd.n.lessons", d.Lessons, undated, inferred))
		if d.Unreadable > 0 {
			n = append(n, Tf("gd.n.unreadable", d.Unreadable))
		}
	}
	if !d.RefusalsMeasured {
		n = append(n, T("gd.n.refusalsNo"))
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
		{Name: T("gd.s.dated"), Ink: Brand, Points: stated},
		{Name: T("gd.s.inferred"), Ink: p[5], Points: inferred},
	}
	v.Rec.Max = toolkit.NiceCeiling(max(peak, 1))
	for _, l := range d.Landings {
		v.Rec.Markers = append(v.Rec.Markers, toolkit.TimeMarker{At: l.At, Ink: p[2]})
	}
	return v
}
