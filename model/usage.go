package model

// Families are the model families the usage charts stack, bottom to top.
var Families = []string{"Opus", "Sonnet", "Haiku", "Fable"}

// Usage is the Quota tab's data: the official usage windows aiquota reports,
// what was recorded of them, and the tokens the local transcripts hold. It is
// computed on the server so the browser draws and never aggregates.
type Usage struct {
	At int64 `json:"at"` // Unix seconds the view was built

	// SessionEnd and WeekEnd are the reset instants (Unix seconds) closing the
	// current 5 hour and 7 day windows; 0 when unknown. ResetSource says where
	// WeekEnd came from: "aiquota", "recorded" (the last reading kept, aiquota
	// being unreadable) or "" (unknown: the weeks end now).
	SessionEnd  int64  `json:"sessionEnd,omitempty"`
	WeekEnd     int64  `json:"weekEnd"`
	ResetSource string `json:"resetSource,omitempty"`
	// QuotaErr is why aiquota could not be read, "" when it could.
	QuotaErr string `json:"quotaErr,omitempty"`
	// RecordingSince is the first instant of the recorded history, 0 when none.
	RecordingSince int64 `json:"recordingSince,omitempty"`

	Families []string      `json:"families"` // stack order, e.g. Opus Sonnet Haiku Fable
	Windows  []QuotaWindow `json:"windows"`
	Short    TokenSeries   `json:"short"` // the 5 hour window, 10 minute buckets
	Week     TokenSeries   `json:"week"`  // the 7 day window, hourly buckets
	Weeks    []WeekTotals  `json:"weeks"` // four 7 day windows, oldest first
}

// QuotaWindow is one official window and its recorded history.
type QuotaWindow struct {
	Label     string  `json:"label"`
	Available bool    `json:"available"`
	Percent   float64 `json:"percent"`
	ResetsAt  int64   `json:"resetsAt,omitempty"`
	// History is the recorded readings, oldest first: at most one point per
	// bucket of the span it is drawn over.
	History []QuotaPoint `json:"history,omitempty"`
}

// QuotaPoint is one recorded reading of a window.
type QuotaPoint struct {
	At      int64   `json:"at"`
	Percent float64 `json:"percent"`
}

// TokenSeries is tokens per bucket per family: Out[family][bucket], and the
// same for cache reads. Bucket i covers [Start+i*Step, Start+(i+1)*Step).
type TokenSeries struct {
	Start     int64       `json:"start"`
	Step      int64       `json:"step"`
	Out       [][]float64 `json:"out"`
	CacheRead [][]float64 `json:"cacheRead"`
}

// WeekTotals is one 7 day window aligned on the weekly reset.
type WeekTotals struct {
	Start, End int64 // [Start, End)
	InProgress bool
	Out        []float64 // per family
	CacheRead  []float64 // per family
	// CumOut and CumCacheRead are the running totals over all families at
	// 6 hour steps from Start; a window in progress stops at the present.
	CumOut       []float64
	CumCacheRead []float64
	// Percent is the last official weekly percentage recorded in the window;
	// Recorded is false when none was.
	Percent  float64
	Recorded bool
}
