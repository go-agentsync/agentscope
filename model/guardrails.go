package model

// Guardrails is the Guardrails tab's data: whether the fleet's gates work. It is
// computed on the server, so the browser draws and never aggregates.
//
// Two sources feed it and either can be missing: `recur --json` (the lessons, their
// recurrences and the ledger of decisions) and the transcripts (the refusals a
// gate made). A source that could not be read is "not measured" -- never zero.
type Guardrails struct {
	At int64 `json:"at"` // Unix seconds the view was built

	// Recur is whether `recur` was read, and why not when it was not.
	RecurMeasured bool   `json:"recurMeasured"`
	RecurErr      string `json:"recurErr,omitempty"`
	// RefusalsMeasured is whether the transcripts have been scanned at all.
	RefusalsMeasured bool `json:"refusalsMeasured"`

	// Totals is what recur counted (zero values when RecurMeasured is false).
	Lessons    int `json:"lessons"`
	Untriaged  int `json:"untriaged"`
	Unreadable int `json:"unreadable"`

	// Leaking is the number of landed gates (decision "gate") with a recurrence
	// dated after the day they landed. SilentGates is the number of landed gates
	// that look obsolete or switched off (see Lesson.Silent).
	Leaking     int `json:"leaking"`
	SilentGates int `json:"silentGates"`

	// Weeks are the Monday 00:00 (local) instants the 12 weeks start at, oldest
	// first; the last is the current, partial week.
	Weeks []int64 `json:"weeks"`
	// RecStated and RecInferred are the dated recurrences per week: those whose
	// year the memory states, and those whose year `recur` inferred.
	RecStated   []float64 `json:"recStated"`
	RecInferred []float64 `json:"recInferred"`
	// Gates names the refusing gates, the stacked layers' order; Refusals[g][w] is
	// gate g's refusals in week w.
	Gates    []string    `json:"gates"`
	Refusals [][]float64 `json:"refusals"`
	// ThisWeek and LastWeek are the refusals of the last two weeks, all gates.
	ThisWeek int `json:"thisWeek"`
	LastWeek int `json:"lastWeek"`
	// Landings are the instants a gate was written into the ledger.
	Landings []Landing `json:"landings,omitempty"`

	Rows []GuardLesson `json:"rows"`
}

// Landing is a gate written into the ledger at At (Unix seconds).
type Landing struct {
	At     int64  `json:"at"`
	Lesson string `json:"lesson"`
}

// GuardLesson is one lesson of the ledger.
type GuardLesson struct {
	Name     string `json:"name"`
	Decision string `json:"decision"`
	// GateText is the ledger's text, cut to a bounded length.
	GateText string `json:"gateText,omitempty"`
	// WrittenAt is when the decision was recorded (Unix seconds), 0 for none.
	WrittenAt int64 `json:"writtenAt,omitempty"`

	// Total counts every recurrence line; Dated those with a date (Inferred of
	// them with a year `recur` guessed) and Undated the rest.
	Total    int `json:"total"`
	Dated    int `json:"dated"`
	Inferred int `json:"inferred"`
	Undated  int `json:"undated"`
	// Last is the date of the latest dated recurrence, "" for none.
	Last string `json:"last,omitempty"`
	// After counts the recurrences dated after the day the decision was written
	// (AfterInferred of them with an inferred year); 0 when nothing was written.
	After         int `json:"after"`
	AfterInferred int `json:"afterInferred"`
	// Silent is a landed gate, at least SilentWeeks old, whose named gates made no
	// refusal in those weeks and that saw no recurrence since it landed: either
	// obsolete or no longer running. Gated says which refusing gates the ledger
	// text names, so the flag is only ever raised for a gate that can be counted.
	Silent bool     `json:"silent,omitempty"`
	Named  []string `json:"named,omitempty"`

	Lines []RecLine `json:"lines,omitempty"`
}

// RecLine is one recurrence sentence of a lesson.
type RecLine struct {
	Date     string `json:"date,omitempty"`
	Inferred bool   `json:"inferred,omitempty"`
	Text     string `json:"text"`
}
