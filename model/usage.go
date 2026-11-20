package model

// Families are the model families the usage charts stack, bottom to top.
var Families = []string{"Opus", "Sonnet", "Haiku", "Fable"}

// UnattributedLayer is the name of the extra layer of the aggregated charts:
// tokens of an API call that no recorded account can be given.
const UnattributedLayer = "unattributed"

// Usage is the Quota tab's data: every account's official usage windows, what was
// recorded of them, and the tokens the local transcripts hold, per account and
// summed. It is computed on the server so the browser draws and never aggregates.
// An account is named by a short hash and a label, never by its e-mail or id.
type Usage struct {
	At       int64    `json:"at"` // Unix seconds the view was built
	Families []string `json:"families"`
	// Active is the key of the account Claude Code is signed in to now, "" when
	// it cannot be told.
	Active string `json:"active,omitempty"`
	// QuotaErr is why aiquota could not be read, "" when it could.
	QuotaErr string `json:"quotaErr,omitempty"`

	Accounts []AccountUsage `json:"accounts"`
	Capacity Capacity       `json:"capacity"`
	// All is the tokens of every account summed, with an extra last layer for
	// what could not be attributed (Layers names them).
	All AllTokens `json:"all"`
}

// AccountUsage is one account's view.
type AccountUsage struct {
	// Key is the account's stable key: a short hash of its id; "" is the account
	// of readings recorded before accounts were told apart (unknown, never
	// guessed).
	Key    string `json:"key"`
	Label  string `json:"label"`
	Plan   string `json:"plan,omitempty"`
	Active bool   `json:"active,omitempty"`
	// Alias is the display name the owner gave the account (accounts.toml); it
	// replaces the label wherever the account is named. "" when it has none.
	Alias string `json:"alias,omitempty"`
	// Suggest is the organisation's name, offered as a first value of the rename
	// dialog. It is sent ONLY when the server runs with --show-org, and only for
	// the account Claude Code is signed in to.
	Suggest string `json:"suggest,omitempty"`

	// SessionEnd and WeekEnd are the reset instants (Unix seconds) closing the
	// current 5 hour and 7 day windows; 0 when unknown. ResetSource says where
	// WeekEnd came from: "aiquota", "recorded" (the last reading kept) or ""
	// (unknown: the weeks end now).
	SessionEnd     int64  `json:"sessionEnd,omitempty"`
	WeekEnd        int64  `json:"weekEnd"`
	ResetSource    string `json:"resetSource,omitempty"`
	RecordingSince int64  `json:"recordingSince,omitempty"`

	Windows []QuotaWindow `json:"windows"`
	Short   TokenSeries   `json:"short"` // the 5 hour window, 10 minute buckets
	Week    TokenSeries   `json:"week"`  // the 7 day window, hourly buckets
	Weeks   []WeekTotals  `json:"weeks"` // four 7 day windows, oldest first
}

// AllTokens is the sum over accounts.
type AllTokens struct {
	Layers []string    `json:"layers"` // Families, then UnattributedLayer
	Short  TokenSeries `json:"short"`
	Week   TokenSeries `json:"week"`
	// Days is 28 calendar days (local midnight to midnight), the last ending at
	// the end of today.
	Days TokenSeries `json:"days"`
	// Unattributed is the tokens (output, cache read) that no account got.
	UnattributedOut   float64 `json:"unattributedOut"`
	UnattributedCache float64 `json:"unattributedCache"`
}

// Capacity is what is left of the weekly windows, summed over accounts.
type Capacity struct {
	// AccountWeeks is the sum of (100 - weekly %)/100 over the accounts whose
	// weekly window is current; Counted is how many that is.
	AccountWeeks float64        `json:"accountWeeks"`
	Counted      int            `json:"counted"`
	Items        []CapacityItem `json:"items"`
}

// CapacityItem is one account's row of the capacity table.
type CapacityItem struct {
	Key         string  `json:"key"`
	Label       string  `json:"label"`
	Alias       string  `json:"alias,omitempty"`
	Plan        string  `json:"plan,omitempty"`
	Active      bool    `json:"active,omitempty"`
	SessionOK   bool    `json:"sessionOk,omitempty"`
	Session     float64 `json:"session,omitempty"`
	WeeklyOK    bool    `json:"weeklyOk,omitempty"`
	Weekly      float64 `json:"weekly,omitempty"`
	NextReset   int64   `json:"nextReset,omitempty"`
	SessionNext int64   `json:"sessionNext,omitempty"`
	// Stale is a reading whose reset has passed: the account is listed, not
	// counted.
	Stale bool `json:"stale,omitempty"`
	// Unknown is the account of readings recorded before accounts were told
	// apart: it may be one of the others, so it is listed and never counted.
	Unknown bool `json:"unknown,omitempty"`
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

// TokenSeries is tokens per bucket per layer: Out[layer][bucket], and the same
// for cache reads. Bucket i covers [Start+i*Step, Start+(i+1)*Step).
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
