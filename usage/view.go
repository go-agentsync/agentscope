package usage

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/go-agentsync/agentscope/accounts"
	"github.com/go-agentsync/agentscope/guard"
	"github.com/go-agentsync/agentscope/model"
	"github.com/go-agentsync/agentscope/quota"
)

const (
	week       = 7 * 24 * 3600
	day        = 24 * 3600
	shortSpan  = 5 * 3600
	cumStep    = 6 * 3600 // step of the cumulative lines
	weeksShown = 4
	daysShown  = 28
	hourSec    = 3600
)

// Input is everything Build needs besides the transcripts.
type Input struct {
	Now time.Time
	// Live is the last successful aiquota reading, one per account, tagged; empty
	// when aiquota has not been read (or could not be).
	Live []quota.Reading
	// Hist is every recorded reading (live ones included), oldest first.
	Hist []quota.Reading
	// QuotaErr is why aiquota could not be read.
	QuotaErr string
	// Active is the key of the account Claude Code is signed in to, "" unknown.
	Active string
	// Alias is the display name of an account by key ("" for none); nil is none.
	Alias func(key string) string
	// Color is the colour the owner gave an account by key ("" for none); nil is
	// none.
	Color func(key string) string
	// Prefs is the viewer's theme and language; the zero value is none chosen.
	Prefs model.Prefs
	// Stamps is the `stamp` hook's timeline: an account known only by its tokens
	// takes its label and plan from its latest stamp.
	Stamps accounts.Timeline
	// Suggest is the organisation's name of the active account, "" unless the
	// server runs with --show-org.
	Suggest string
}

// Build assembles the Quota tab's data: one view per account, the sum, and the
// capacity left.
func Build(in Input, sc *Scanner) *model.Usage {
	n := in.Now.Unix()
	u := &model.Usage{At: n, Families: Families, Active: in.Active, QuotaErr: in.QuotaErr, Prefs: in.Prefs}

	// Every reading, once: a live one is also in the history once it is recorded.
	type rk struct {
		acct string
		at   int64
	}
	seen := map[rk]bool{}
	byAcct := map[string][]quota.Reading{}
	live := map[string]int64{}
	for _, r := range in.Live {
		live[r.Acct] = r.At
	}
	for _, r := range append(append([]quota.Reading(nil), in.Hist...), in.Live...) {
		if k := (rk{r.Acct, r.At}); !seen[k] {
			seen[k] = true
			byAcct[r.Acct] = append(byAcct[r.Acct], r)
		}
	}
	keys := map[string]bool{}
	for k, rs := range byAcct {
		sort.SliceStable(rs, func(i, j int) bool { return rs[i].At < rs[j].At })
		keys[k] = true
	}
	tokenKeys, _ := sc.Accounts()
	for _, k := range tokenKeys {
		keys[k] = true
	}
	var order []string
	for k := range keys {
		order = append(order, k)
	}
	// Known accounts by key, the unknown one ("") last.
	sort.Slice(order, func(i, j int) bool {
		if (order[i] == "") != (order[j] == "") {
			return order[j] == ""
		}
		return order[i] < order[j]
	})

	for _, k := range order {
		u.Accounts = append(u.Accounts, buildAccount(n, k, byAcct[k], live[k] != 0 && live[k] == lastAt(byAcct[k]) && in.QuotaErr == "", sc))
	}
	for i := range u.Accounts {
		a := &u.Accounts[i]
		a.Active = in.Active != "" && a.Key == in.Active
		if in.Alias != nil && a.Key != "" {
			a.Alias = in.Alias(a.Key)
		}
		if in.Color != nil && a.Key != "" {
			a.Color = in.Color(a.Key)
		}
		if st, ok := in.Stamps.Latest(a.Key); ok && a.Key != "" {
			// An account no aiquota reading names is known by what the hook saw.
			if a.Label == "account" && st.Label != "" {
				a.Label = st.Label
			}
			if a.Plan == "" {
				a.Plan = st.Plan
			}
		}
		if a.Active && in.Suggest != "" {
			a.Suggest = in.Suggest
		}
	}

	// The sum: the last 5 hours, the last 7 days and 28 calendar days.
	end := n - n%BucketSec + BucketSec
	u.All.Layers = append(append([]string(nil), Families...), model.UnattributedLayer)
	u.All.Short = series(sc, end-shortSpan, BucketSec, shortSpan/BucketSec, Every, "")
	wend := n - n%hourSec + hourSec
	u.All.Week = series(sc, wend-week, hourSec, week/hourSec, Every, "")
	u.All.Days = series(sc, midnight(in.Now).Unix()+day-daysShown*day, day, daysShown, Every, "")
	last := len(Families)
	for i := range u.All.Days.Out[last] {
		u.All.UnattributedOut += u.All.Days.Out[last][i]
		u.All.UnattributedCache += u.All.Days.CacheRead[last][i]
	}

	u.Capacity = capacity(n, u.Accounts)
	for i := range u.Capacity.Items { // the alias, the colour and the plan follow the account into the table
		for _, a := range u.Accounts {
			if a.Key == u.Capacity.Items[i].Key {
				u.Capacity.Items[i].Alias, u.Capacity.Items[i].Plan, u.Capacity.Items[i].Color = a.Alias, a.Plan, a.Color
			}
		}
	}
	return u
}

func series(sc *Scanner, start, step int64, n int, w Which, acct string) model.TokenSeries {
	out, cr := sc.Buckets(start, step, n, w, acct)
	return model.TokenSeries{Start: start, Step: step, Out: out, CacheRead: cr}
}

// midnight is the start of the local day of t.
func midnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func lastAt(rs []quota.Reading) int64 {
	if len(rs) == 0 {
		return 0
	}
	return rs[len(rs)-1].At
}

func floor(v, step int64) int64 { return v - v%step }

// buildAccount is one account's view: its windows, its reset, its tokens. rs is
// the account's readings, oldest first; fresh says the newest is a live reading.
func buildAccount(n int64, key string, rs []quota.Reading, fresh bool, sc *Scanner) model.AccountUsage {
	a := model.AccountUsage{Key: key, Label: "unknown account"}
	if key != "" {
		a.Label = "account"
	}
	var latest quota.Reading
	have := len(rs) > 0
	if have {
		latest = rs[len(rs)-1]
		if latest.Label != "" {
			a.Label = latest.Label
		}
		a.Plan = latest.Plan
		a.RecordingSince = rs[0].At
	}
	// The reset the weeks are aligned on, and where it came from.
	if have {
		if w, ok := latest.Find(quota.LabelWeek); ok && w.Available && w.ResetsAt > 0 {
			a.WeekEnd, a.ResetSource = w.ResetsAt, "recorded"
			if fresh {
				a.ResetSource = "aiquota"
			}
		}
		if w, ok := latest.Find(quota.LabelSession); ok && w.Available && w.ResetsAt > n {
			a.SessionEnd = w.ResetsAt
		}
	}
	for i := len(rs) - 1; i >= 0 && a.WeekEnd == 0; i-- {
		if w, ok := rs[i].Find(quota.LabelWeek); ok && w.Available && w.ResetsAt > 0 {
			a.WeekEnd, a.ResetSource = w.ResetsAt, "recorded"
		}
	}
	switch {
	case a.WeekEnd == 0:
		a.WeekEnd = n
	case a.WeekEnd <= n:
		// A stale reset: the window rolled over since. Step to the next one.
		a.WeekEnd += ((n-a.WeekEnd)/week + 1) * week
	}

	// The tokens of this account alone: the unknown account has none.
	tok := key
	if key == "" {
		tok = "\x00"
	}
	end := a.SessionEnd
	if end == 0 {
		end = n - n%BucketSec + BucketSec
	}
	a.Short = series(sc, end-shortSpan, BucketSec, shortSpan/BucketSec, Only, tok)
	a.Week = series(sc, floor(a.WeekEnd-week, hourSec), hourSec, week/hourSec, Only, tok)

	// Four weeks aligned on the reset, oldest first.
	first := floor(a.WeekEnd-weeksShown*week, BucketSec)
	perWeek := week / cumStep
	out, cr := sc.Buckets(first, cumStep, weeksShown*perWeek, Only, tok)
	for k := 0; k < weeksShown; k++ {
		w := model.WeekTotals{Start: a.WeekEnd - int64(weeksShown-k)*week}
		w.End = w.Start + week
		w.InProgress = n >= w.Start && n < w.End
		w.Out, w.CacheRead = make([]float64, len(Families)), make([]float64, len(Families))
		var runOut, runCR float64
		for i := 0; i < perWeek; i++ {
			if w.Start+int64(i)*cumStep > n { // not started yet: nothing to draw
				break
			}
			for f := range Families {
				o, c := out[f][k*perWeek+i], cr[f][k*perWeek+i]
				w.Out[f] += o
				w.CacheRead[f] += c
				runOut += o
				runCR += c
			}
			w.CumOut = append(w.CumOut, runOut)
			w.CumCacheRead = append(w.CumCacheRead, runCR)
		}
		w.Percent, w.Recorded = weekPercent(rs, w.Start, w.End)
		a.Weeks = append(a.Weeks, w)
	}

	// The official windows, with their recorded history.
	if have {
		for _, w := range latest.Windows {
			qw := model.QuotaWindow{Label: w.Label, Available: w.Available, Percent: w.Percent, ResetsAt: w.ResetsAt}
			if w.Available {
				qw.History = history(rs, w.Label, n, w.Label == quota.LabelSession)
			}
			a.Windows = append(a.Windows, qw)
		}
	}
	return a
}

// weekPercent is the last official weekly percentage recorded in [start, end].
func weekPercent(rs []quota.Reading, start, end int64) (float64, bool) {
	var p float64
	var ok bool
	var at int64
	for _, r := range rs {
		if r.At < start || r.At > end || r.At < at {
			continue
		}
		if w, found := r.Find(quota.LabelWeek); found && w.Available {
			p, ok, at = w.Percent, true, r.At
		}
	}
	return p, ok
}

// history is the readings of one window over the last 28 days, thinned to one
// per hour (the last of the hour); the session window keeps every reading of the
// last 6 hours instead.
func history(rs []quota.Reading, label string, now int64, fine bool) []model.QuotaPoint {
	var out []model.QuotaPoint
	for _, r := range rs {
		w, ok := r.Find(label)
		if !ok || !w.Available {
			continue
		}
		age := now - r.At
		if age < 0 || age > weeksShown*week {
			continue
		}
		if fine && age > 6*hourSec {
			continue
		}
		p := model.QuotaPoint{At: r.At, Percent: w.Percent}
		if !fine && len(out) > 0 && out[len(out)-1].At/hourSec == p.At/hourSec {
			out[len(out)-1] = p
			continue
		}
		out = append(out, p)
	}
	return out
}

// capacity sums what is left of the weekly windows, in account-weeks: an account
// at 25 % has 0.75 of a week left. A reading whose reset has passed says nothing
// about now, so its account is listed and not counted.
func capacity(n int64, accts []model.AccountUsage) model.Capacity {
	var c model.Capacity
	for _, a := range accts {
		if len(a.Windows) == 0 {
			continue
		}
		it := model.CapacityItem{Key: a.Key, Label: a.Label, Active: a.Active}
		for _, w := range a.Windows {
			if !w.Available {
				continue
			}
			switch w.Label {
			case quota.LabelSession:
				it.SessionOK, it.Session, it.SessionNext = true, w.Percent, w.ResetsAt
			case quota.LabelWeek:
				it.WeeklyOK, it.Weekly, it.NextReset = true, w.Percent, w.ResetsAt
			}
		}
		if it.SessionOK && it.SessionNext <= n {
			it.SessionOK = false // a 5 hour window that has rolled over
		}
		if it.WeeklyOK && it.NextReset <= n {
			it.WeeklyOK, it.Stale = false, true
		}
		it.Unknown = a.Key == ""
		if it.WeeklyOK && !it.Unknown {
			c.AccountWeeks += (100 - it.Weekly) / 100
			c.Counted++
		}
		c.Items = append(c.Items, it)
	}
	return c
}

// Engine keeps the Quota tab's data current: it scans transcripts every
// ScanEvery, reads aiquota at most every quota.MinInterval, and records each
// reading with the account it belongs to.
type Engine struct {
	scan    *Scanner
	store   *quota.Store
	run     quota.Runner
	active  func() string
	now     func() time.Time
	stamps  *accounts.Stamps
	aliases *accounts.Aliases
	colors  *accounts.Colors
	prefs   *accounts.Prefs
	// org reads the organisation's name; nil unless --show-org.
	org func() string

	// recur is where the Guardrails tab reads `recur --json` (nil: not configured).
	recur *guard.Source

	mu       sync.Mutex
	view     *model.Usage
	gview    *model.Guardrails
	live     []quota.Reading
	quotaErr string
	lastRead time.Time
}

// ScanEvery is how often transcripts are scanned for new tokens.
const ScanEvery = time.Minute

// scanEvery is the interval Run ticks at; a test shortens it to see a tick.
var scanEvery = ScanEvery

// NewEngine reads transcripts under claudeDir/projects, caches sums in
// cacheDir and records readings in configDir. The signed-in account is read from
// the .claude.json next to claudeDir.
func NewEngine(claudeDir, cacheDir, configDir string, run quota.Runner) *Engine {
	return &Engine{
		scan:   NewScanner(filepath.Join(claudeDir, "projects"), filepath.Join(cacheDir, "usage.json")),
		store:  quota.NewStore(configDir),
		run:    run,
		active: func() string { return ActiveKey(filepath.Join(filepath.Dir(claudeDir), ".claude.json")) },
		now:    time.Now,

		stamps:  accounts.NewStamps(configDir),
		aliases: accounts.NewAliases(configDir),
		colors:  accounts.NewColors(configDir),
		prefs:   accounts.NewPrefs(configDir),
	}
}

// ShowOrg makes the engine offer the organisation's name (from the .claude.json
// next to claudeDir) as a suggested display name of the active account: it is
// off by default, and while it is off no organisation name is read at all.
func (e *Engine) ShowOrg(claudeDir string) {
	path := filepath.Join(filepath.Dir(claudeDir), ".claude.json")
	e.org = func() string {
		id, _ := accounts.ReadIdentity(path, true)
		return id.Org
	}
}

// Rename gives the account with the key a display name, or removes it when name
// is empty, and rebuilds the view so the next snapshot carries it. It is what
// the UI's Rename action calls; the file is written here, by the serve process.
func (e *Engine) Rename(key, name string) error {
	if err := e.aliases.Set(key, name); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.view != nil { // show it now, not at the next minute's pass
		for i := range e.view.Accounts {
			if a := &e.view.Accounts[i]; a.Key != "" {
				a.Alias = e.aliases.Name(a.Key)
			}
		}
		for i := range e.view.Capacity.Items {
			if it := &e.view.Capacity.Items[i]; it.Key != "" {
				it.Alias = e.aliases.Name(it.Key)
			}
		}
	}
	return nil
}

// SetColor gives the account with the key a colour (#rrggbb), or restores its
// automatic colour when color is empty, and rebuilds the view so the next
// snapshot carries it. It is what the UI's Colour action calls; the file is
// written here, by the serve process, and a value that is not #rrggbb is refused.
func (e *Engine) SetColor(key, color string) error {
	if err := e.colors.Set(key, color); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.view != nil { // show it now, not at the next minute's pass
		for i := range e.view.Accounts {
			if a := &e.view.Accounts[i]; a.Key != "" {
				a.Color = e.colors.Color(a.Key)
			}
		}
		for i := range e.view.Capacity.Items {
			if it := &e.view.Capacity.Items[i]; it.Key != "" {
				it.Color = e.colors.Color(it.Key)
			}
		}
	}
	return nil
}

// SetPrefs stores the viewer's theme ("light", "dark", "system" or "") and
// language (a tag or "" for automatic) and rebuilds the view so the next snapshot
// carries them. It is what the UI's theme and language selectors call; the file
// is written here, by the serve process, and a value that is neither is refused.
func (e *Engine) SetPrefs(theme, lang string) error {
	if err := e.prefs.Set(theme, lang); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.view != nil { // show it now, not at the next minute's pass
		e.view.Prefs = e.prefs.Get()
	}
	return nil
}

// input is what Build needs besides the transcripts, at now. The caller holds
// e.mu.
func (e *Engine) input(now time.Time) Input {
	in := Input{Now: now, Live: e.live, QuotaErr: e.quotaErr, Active: e.active(), Alias: e.aliases.Name, Color: e.colors.Color, Prefs: e.prefs.Get(), Stamps: e.stamps.Load()}
	in.Hist, _ = e.store.Load()
	if e.org != nil {
		in.Suggest = e.org()
	}
	return in
}

// ActiveKey is the key of the account Claude Code is signed in to, from the
// oauthAccount of its .claude.json: "" when that cannot be read. Only the
// account id is decoded, and only its hash is kept.
func ActiveKey(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	var c struct {
		OAuth struct {
			ID string `json:"accountUuid"`
		} `json:"oauthAccount"`
	}
	if json.NewDecoder(io.LimitReader(f, 32<<20)).Decode(&c) != nil {
		return ""
	}
	return quota.AccountKey(c.OAuth.ID)
}

// Step does one pass at now.
func (e *Engine) Step(ctx context.Context, now time.Time) {
	hist, _ := e.store.Load()
	if e.lastRead.IsZero() || now.Sub(e.lastRead) >= quota.MinInterval {
		e.lastRead = now
		before := e.active()
		b, err := e.run(ctx)
		var rs []quota.Reading
		if err == nil {
			rs, err = quota.Parse(b)
		}
		// An account that changed during the reading cannot be told.
		tag := before
		if e.active() != before {
			tag = ""
		}
		e.mu.Lock()
		if err != nil {
			e.quotaErr = err.Error()
		} else {
			e.quotaErr, e.live = "", rs
			quota.Tag(e.live, tag)
			for _, r := range e.live {
				if appendErr := e.store.Append(r); appendErr != nil {
					e.quotaErr = "history not recorded: " + appendErr.Error()
				}
			}
			hist, _ = e.store.Load()
		}
		e.mu.Unlock()
	}
	e.aliases.Reload() // the owner may have edited the file by hand
	e.colors.Reload()
	e.scan.SetStamps(e.stamps.Load())
	e.scan.Refresh(now)
	g := guard.Build(e.guardInput(ctx, now))
	e.mu.Lock()
	defer e.mu.Unlock()
	in := e.input(now)
	in.Hist = hist
	e.view = Build(in, e.scan)
	e.gview = g
}

// guardInput is what the Guardrails view is built from at now: the transcripts'
// refusals, and what recur says (or why it says nothing).
func (e *Engine) guardInput(ctx context.Context, now time.Time) guard.Input {
	in := guard.Input{Now: now, Refusals: e.scan.Refusals(), Scanned: e.scan.Scanned()}
	if e.recur == nil {
		in.RecErr = "recur is not configured"
		return in
	}
	in.Rec, in.RecErr = e.recur.Get(ctx, now)
	return in
}

// SetRecur gives the engine the source of `recur --json` that the Guardrails tab
// reads (at most every guard.MinInterval).
func (e *Engine) SetRecur(s *guard.Source) { e.recur = s }

// Guardrails is the Guardrails tab's latest data, nil before the first pass ended.
func (e *Engine) Guardrails() *model.Guardrails {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.gview
}

// Run steps now and then every ScanEvery until ctx ends.
func (e *Engine) Run(ctx context.Context) {
	e.Step(ctx, e.now())
	t := time.NewTicker(scanEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.Step(ctx, e.now())
		}
	}
}

// View is the latest data, nil before the first pass ended.
func (e *Engine) View() *model.Usage {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.view
}

// Source adds the engine's view to the snapshots of an inner source.
type Source struct {
	Inner interface{ Snapshot() model.Snapshot }
	Eng   *Engine
}

// Snapshot is the inner snapshot with the Usage view attached.
func (s Source) Snapshot() model.Snapshot {
	snap := s.Inner.Snapshot()
	snap.Usage = s.Eng.View()
	snap.Guardrails = s.Eng.Guardrails()
	return snap
}
