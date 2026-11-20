package guard

import (
	"sort"
	"strings"
	"time"

	"github.com/go-agentsync/agentscope/model"
)

const (
	// Weeks is how many weeks the trend charts show.
	Weeks = 12
	// SilentWeeks is how long a landed gate must have made no refusal, nor seen a
	// recurrence, to be flagged silent.
	SilentWeeks = 4

	day = 24 * time.Hour

	maxGateText = 600
	maxLineText = 300
	maxLines    = 40
)

// Count is the refusals of one gate in one hour: Hour is the Unix second the hour
// starts at.
type Count struct {
	Hour int64
	Gate string
	N    int64
}

// Input is what Build needs.
type Input struct {
	Now time.Time
	// Rec is what recur answered; RecErr is why it could not be read, "" when it
	// could (and then Rec is its answer, however empty). A value and not a
	// pointer: there is no nil to forget.
	Rec    Recurrence
	RecErr string
	// Refusals are the refusals the transcripts hold; Scanned is whether they were
	// scanned at all (false: not measured, which is not zero).
	Refusals []Count
	Scanned  bool
}

// weekStart is the Monday 00:00 of the week containing t, in t's location.
func weekStart(t time.Time) time.Time {
	y, m, d := t.Date()
	mid := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	back := (int(mid.Weekday()) + 6) % 7 // Monday is 0
	return mid.AddDate(0, 0, -back)
}

// span is the shown weeks: their start instants, and where the last one ends.
type span struct {
	starts []int64
	end    int64
}

// weekOf is the index of the week an instant is in, or -1 when it is outside the
// shown weeks.
func (s span) weekOf(at int64) int {
	if at >= s.end {
		return -1 // the future
	}
	for i := len(s.starts) - 1; i >= 0; i-- {
		if at >= s.starts[i] {
			return i
		}
	}
	return -1
}

// Build assembles the tab's data.
func Build(in Input) *model.Guardrails {
	g := &model.Guardrails{At: in.Now.Unix(), RecurMeasured: in.RecErr == "", RecurErr: in.RecErr, RefusalsMeasured: in.Scanned}
	cur := weekStart(in.Now)
	for i := Weeks - 1; i >= 0; i-- {
		g.Weeks = append(g.Weeks, cur.AddDate(0, 0, -7*i).Unix())
	}
	g.RecStated, g.RecInferred = make([]float64, Weeks), make([]float64, Weeks)
	sp := span{starts: g.Weeks, end: cur.AddDate(0, 0, 7).Unix()}

	g.Gates, g.Refusals, g.ThisWeek, g.LastWeek = refusalWeeks(sp, in.Refusals)

	g.Lessons, g.Unreadable = len(in.Rec.Lessons), in.Rec.Totals.Unreadable
	window := in.Now.Add(-SilentWeeks * 7 * day).Unix()
	recent := map[string]int64{} // refusals per gate in the silent window
	for _, c := range in.Refusals {
		if c.Hour >= window {
			recent[c.Gate] += c.N
		}
	}
	for _, l := range in.Rec.Lessons {
		row := lessonRow(l, in.Now, sp, g)
		if row.Decision == "untriaged" {
			g.Untriaged++
		}
		if row.Decision == "gate" {
			if row.After > 0 {
				g.Leaking++
			}
			if in.Scanned {
				row.Named = Named(row.GateText)
				row.Silent = silent(row, in.Now, recent)
				if row.Silent {
					g.SilentGates++
				}
			}
			if row.WrittenAt > 0 {
				g.Landings = append(g.Landings, model.Landing{At: row.WrittenAt, Lesson: row.Name})
			}
		}
		g.Rows = append(g.Rows, row)
	}
	sort.Slice(g.Landings, func(i, j int) bool { return g.Landings[i].At < g.Landings[j].At })
	return g
}

// refusalWeeks turns the hourly counts into one layer per gate that refused at
// all in the shown weeks, in Gates order, and the last two weeks' totals.
func refusalWeeks(sp span, counts []Count) (gates []string, layers [][]float64, this, last int) {
	by := map[string][]float64{}
	for _, c := range counts {
		w := sp.weekOf(c.Hour)
		if w < 0 || c.N <= 0 {
			continue
		}
		if by[c.Gate] == nil {
			by[c.Gate] = make([]float64, len(sp.starts))
		}
		by[c.Gate][w] += float64(c.N)
	}
	for _, name := range Gates {
		if l, ok := by[name]; ok {
			gates, layers = append(gates, name), append(layers, l)
			delete(by, name)
		}
	}
	// A gate name this build does not know (a cache written by a newer one) is
	// shown as itself, after the known ones.
	var extra []string
	for name := range by {
		extra = append(extra, name)
	}
	sort.Strings(extra)
	for _, name := range extra {
		gates, layers = append(gates, name), append(layers, by[name])
	}
	n := len(sp.starts)
	for _, l := range layers {
		this += int(l[n-1])
		last += int(l[n-2])
	}
	return gates, layers, this, last
}

// lessonRow summarises one lesson and adds its dated recurrences to the weekly
// series of g.
func lessonRow(l Lesson, now time.Time, sp span, g *model.Guardrails) model.GuardLesson {
	row := model.GuardLesson{Name: l.Name, Decision: l.Ledger.Decision, GateText: cut(l.Ledger.Text, maxGateText), Total: len(l.Recurrences)}
	writtenDay := ""
	if l.Ledger.WrittenAt != nil {
		if t, err := time.Parse(time.RFC3339, *l.Ledger.WrittenAt); err == nil {
			row.WrittenAt = t.Unix()
			writtenDay = t.In(now.Location()).Format(time.DateOnly)
		}
	}
	for _, r := range l.Recurrences {
		line := model.RecLine{Text: cut(r.Line, maxLineText), Inferred: r.YearInferred}
		if r.Date == nil {
			row.Undated++
		} else {
			line.Date = *r.Date
			row.Dated++
			if r.YearInferred {
				row.Inferred++
			}
			if line.Date > row.Last {
				row.Last = line.Date
			}
			if t, err := time.ParseInLocation(time.DateOnly, line.Date, now.Location()); err == nil {
				if w := sp.weekOf(t.Unix()); w >= 0 {
					if r.YearInferred {
						g.RecInferred[w]++
					} else {
						g.RecStated[w]++
					}
				}
			}
			// A recurrence on the day the decision was written is not "after": it may
			// be the one that prompted it.
			if writtenDay != "" && line.Date > writtenDay {
				row.After++
				if r.YearInferred {
					row.AfterInferred++
				}
			}
		}
		if len(row.Lines) < maxLines {
			row.Lines = append(row.Lines, line)
		}
	}
	return row
}

// silent is whether a landed gate looks obsolete or no longer running: it landed
// at least SilentWeeks ago, no recurrence is dated since, the refusing gates its
// text names made no refusal in the window, and there is at least one such gate
// (a gate that is a test or a CI step cannot be counted, so it is never flagged).
func silent(r model.GuardLesson, now time.Time, recent map[string]int64) bool {
	if r.WrittenAt == 0 || len(r.Named) == 0 || r.After > 0 {
		return false
	}
	if now.Sub(time.Unix(r.WrittenAt, 0)) < SilentWeeks*7*day {
		return false
	}
	for _, name := range r.Named {
		if recent[name] > 0 {
			return false
		}
	}
	return true
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// A cut inside a UTF-8 sequence leaves a partial one at the end: drop it.
	return strings.ToValidUTF8(s[:min(n, len(s))], "") + "..."
}
