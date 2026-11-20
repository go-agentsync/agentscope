package usage

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-agentsync/agentscope/quota"
)

const (
	week       = 7 * 24 * 3600
	shortSpan  = 5 * 3600
	cumStep    = 6 * 3600 // step of the cumulative lines
	weeksShown = 4
	hourSec    = 3600
)

// Build assembles the Quota tab's data at now. latest is the newest official
// reading (live, or the last recorded one when quotaErr says aiquota could not
// be read); hist is every recorded reading, oldest first.
func Build(now time.Time, latest quota.Reading, haveLatest bool, quotaErr string, hist []quota.Reading, sc *Scanner) *model.Usage {
	n := now.Unix()
	u := &model.Usage{At: n, Families: Families, QuotaErr: quotaErr}
	if len(hist) > 0 {
		u.RecordingSince = hist[0].At
	}

	// The weekly reset the weeks are aligned on, and where it came from.
	if haveLatest {
		if w, ok := latest.Find(quota.LabelWeek); ok && w.Available && w.ResetsAt > 0 {
			u.WeekEnd, u.ResetSource = w.ResetsAt, "aiquota"
			if quotaErr != "" {
				u.ResetSource = "recorded"
			}
		}
		if w, ok := latest.Find(quota.LabelSession); ok && w.Available && w.ResetsAt > n {
			u.SessionEnd = w.ResetsAt
		}
	}
	if u.WeekEnd == 0 {
		for i := len(hist) - 1; i >= 0 && u.WeekEnd == 0; i-- {
			if w, ok := hist[i].Find(quota.LabelWeek); ok && w.Available && w.ResetsAt > 0 {
				u.WeekEnd, u.ResetSource = w.ResetsAt, "recorded"
			}
		}
	}
	switch {
	case u.WeekEnd == 0:
		u.WeekEnd = n
	case u.WeekEnd <= n:
		// A stale reset: the window rolled over since. Step to the next one.
		u.WeekEnd += ((n-u.WeekEnd)/week + 1) * week
	}

	// 5 hour window: [SessionEnd-5h, SessionEnd), or the last 5 hours.
	end := u.SessionEnd
	if end == 0 {
		end = n - n%BucketSec + BucketSec
	}
	u.Short.Start, u.Short.Step = end-shortSpan, BucketSec
	u.Short.Out, u.Short.CacheRead = sc.Buckets(u.Short.Start, u.Short.Step, shortSpan/BucketSec)

	// 7 day window, hourly.
	u.Week.Start, u.Week.Step = floor(u.WeekEnd-week, hourSec), hourSec
	u.Week.Out, u.Week.CacheRead = sc.Buckets(u.Week.Start, u.Week.Step, week/hourSec)

	// Four weeks aligned on the reset, oldest first.
	first := floor(u.WeekEnd-weeksShown*week, BucketSec)
	perWeek := week / cumStep
	out, cr := sc.Buckets(first, cumStep, weeksShown*perWeek)
	for k := 0; k < weeksShown; k++ {
		w := model.WeekTotals{Start: u.WeekEnd - int64(weeksShown-k)*week}
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
		w.Percent, w.Recorded = weekPercent(hist, latest, haveLatest, w.Start, w.End)
		u.Weeks = append(u.Weeks, w)
	}

	// The official windows, with their recorded history.
	rs := hist
	if haveLatest {
		rs = append(append([]quota.Reading(nil), hist...), latest)
	}
	for _, w := range latest.Windows {
		qw := model.QuotaWindow{Label: w.Label, Available: w.Available, Percent: w.Percent, ResetsAt: w.ResetsAt}
		if w.Available {
			qw.History = history(rs, w.Label, n, w.Label == quota.LabelSession)
		}
		u.Windows = append(u.Windows, qw)
	}
	return u
}

func floor(v, step int64) int64 { return v - v%step }

// weekPercent is the last official weekly percentage recorded in [start, end].
func weekPercent(hist []quota.Reading, latest quota.Reading, have bool, start, end int64) (float64, bool) {
	rs := hist
	if have {
		rs = append(append([]quota.Reading(nil), hist...), latest)
	}
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

// Engine keeps the Quota tab's data current: it scans transcripts every
// ScanEvery, reads aiquota at most every quota.MinInterval, and records each
// reading.
type Engine struct {
	scan  *Scanner
	store *quota.Store
	run   quota.Runner
	now   func() time.Time

	mu       sync.Mutex
	view     *model.Usage
	latest   quota.Reading
	have     bool
	quotaErr string
	lastRead time.Time
}

// ScanEvery is how often transcripts are scanned for new tokens.
const ScanEvery = time.Minute

// NewEngine reads transcripts under claudeDir/projects, caches sums in
// cacheDir and records readings in configDir.
func NewEngine(claudeDir, cacheDir, configDir string, run quota.Runner) *Engine {
	return &Engine{
		scan:  NewScanner(filepath.Join(claudeDir, "projects"), filepath.Join(cacheDir, "usage.json")),
		store: quota.NewStore(configDir),
		run:   run,
		now:   time.Now,
	}
}

// Step does one pass at now.
func (e *Engine) Step(ctx context.Context, now time.Time) {
	hist, _ := e.store.Load()
	if e.lastRead.IsZero() || now.Sub(e.lastRead) >= quota.MinInterval {
		e.lastRead = now
		b, err := e.run(ctx)
		var r quota.Reading
		if err == nil {
			r, err = quota.Parse(b)
		}
		e.mu.Lock()
		if err != nil {
			e.quotaErr = err.Error()
			if !e.have && len(hist) > 0 {
				e.latest, e.have = hist[len(hist)-1], true
			}
		} else {
			e.quotaErr, e.latest, e.have = "", r, true
			if appendErr := e.store.Append(r); appendErr != nil {
				e.quotaErr = "history not recorded: " + appendErr.Error()
			}
			hist, _ = e.store.Load()
		}
		e.mu.Unlock()
	}
	e.scan.Refresh(now)
	e.mu.Lock()
	defer e.mu.Unlock()
	// The newest reading is also the last of hist after an append: the view must
	// not draw it twice.
	if e.have && len(hist) > 0 && hist[len(hist)-1].At == e.latest.At {
		hist = hist[:len(hist)-1]
	}
	e.view = Build(now, e.latest, e.have, e.quotaErr, hist, e.scan)
}

// Run steps now and then every ScanEvery until ctx ends.
func (e *Engine) Run(ctx context.Context) {
	e.Step(ctx, e.now())
	t := time.NewTicker(ScanEvery)
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
	return snap
}
