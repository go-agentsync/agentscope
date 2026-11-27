// Package demo renders the agentscope screen from a SYNTHETIC, deterministic
// scenario into one animated SVG, for demo videos and documentation.
//
// The scenario drives the SAME view-model as the live screen (ui.ViewModel): a
// step applies a snapshot, switches a tab, picks a theme or a language, exactly
// as the browser or the server would. Nothing is drawn by hand and nothing is
// read from this machine: every account, plan, session and repository below is
// invented, and the clock is a constant, so the same scenario yields the same
// bytes on every run.
package demo

import (
	"fmt"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-agentsync/agentscope/quota"
)

// Epoch is the demo's fixed "now". Nothing in the output reads the real clock.
var Epoch = time.Date(2030, 1, 15, 10, 0, 0, 0, time.UTC)

// The invented accounts. Their keys are not hashes of anything real.
const (
	keyLab      = "d3m0a001"
	keyPersonal = "d3m0b002"
)

// series is a deterministic token series: integer arithmetic only, so no
// platform's maths library can move a byte.
func series(layers, n int, scale float64) [][]float64 {
	out := make([][]float64, layers)
	for f := range out {
		out[f] = make([]float64, n)
		for i := range out[f] {
			out[f][i] = scale * float64((i*37+f*11+7)%17+1) * float64(f+1) / 4
		}
	}
	return out
}

func account(key, alias, plan string, weekEnd int64, sessionPct, weekPct float64, active bool) model.AccountUsage {
	const wk = 7 * 24 * 3600
	sessEnd := Epoch.Unix() + 2*3600
	a := model.AccountUsage{
		Key: key, Label: "Claude Code", Alias: alias, Plan: plan, Active: active,
		SessionEnd: sessEnd, WeekEnd: weekEnd, ResetSource: "aiquota", RecordingSince: weekEnd - 5*86400,
		Short: model.TokenSeries{Start: sessEnd - 8*3600, Step: 600, Out: series(4, 48, 100), CacheRead: series(4, 48, 5000)},
		Week:  model.TokenSeries{Start: weekEnd - wk, Step: 3600, Out: series(4, 168, 10), CacheRead: series(4, 168, 900)},
		Windows: []model.QuotaWindow{
			{Label: quota.LabelSession, Available: true, Percent: sessionPct, ResetsAt: sessEnd, History: []model.QuotaPoint{
				{At: sessEnd - 4*3600, Percent: sessionPct / 4}, {At: sessEnd - 2*3600, Percent: sessionPct / 2}, {At: sessEnd - 3600, Percent: sessionPct},
			}},
			{Label: quota.LabelWeek, Available: true, Percent: weekPct, ResetsAt: weekEnd, History: []model.QuotaPoint{
				{At: weekEnd - 6*86400, Percent: weekPct / 5}, {At: weekEnd - 4*86400, Percent: weekPct / 2}, {At: weekEnd - 86400, Percent: weekPct},
			}},
		},
	}
	for k := 0; k < 4; k++ {
		w := model.WeekTotals{Start: weekEnd - int64(4-k)*wk, InProgress: k == 3,
			Out: []float64{4e6 + float64(k)*5e5, 3e6, 2e6, 1e6}, CacheRead: []float64{4e9, 3e9, 2e9, 1e9}}
		w.End = w.Start + wk
		for i := 0; i < 6+k; i++ {
			w.CumOut = append(w.CumOut, float64(i*(k+1))*1e6)
			w.CumCacheRead = append(w.CumCacheRead, float64(i*(k+1))*1e9)
		}
		w.Recorded, w.Percent = k%2 == 1, float64(15*(k+1))
		a.Weeks = append(a.Weeks, w)
	}
	return a
}

// Usage is the Quota tab's data: two invented accounts. A non-empty colour is
// the one the owner gave that account.
func Usage(labColor, personalColor string) *model.Usage {
	now := Epoch.Unix()
	wkA, wkB := now+2*86400, now+4*86400+7200
	a := account(keyLab, "Acme Lab", "team", wkA, 38, 61, true)
	b := account(keyPersonal, "Personal", "max", wkB, 12, 24, false)
	a.Color, b.Color = labColor, personalColor
	const wk = 7 * 24 * 3600
	return &model.Usage{
		At: now, Families: model.Families, Active: keyLab,
		Accounts: []model.AccountUsage{a, b},
		Capacity: model.Capacity{AccountWeeks: 1.15, Counted: 2, Items: []model.CapacityItem{
			{Key: keyLab, Label: "Claude Code", Alias: "Acme Lab", Plan: "team", Color: labColor, Active: true, SessionOK: true, Session: 38, WeeklyOK: true, Weekly: 61, NextReset: wkA, SessionNext: a.SessionEnd},
			{Key: keyPersonal, Label: "Claude Code", Alias: "Personal", Plan: "max", Color: personalColor, SessionOK: true, Session: 12, WeeklyOK: true, Weekly: 24, NextReset: wkB, SessionNext: b.SessionEnd},
		}},
		All: model.AllTokens{
			Layers: append(append([]string(nil), model.Families...), model.UnattributedLayer),
			Short:  model.TokenSeries{Start: now - 8*3600, Step: 600, Out: series(5, 48, 100), CacheRead: series(5, 48, 5000)},
			Week:   model.TokenSeries{Start: now - wk, Step: 3600, Out: series(5, 168, 10), CacheRead: series(5, 168, 900)},
			Days:   model.TokenSeries{Start: now - 28*86400, Step: 86400, Out: series(5, 28, 1e6), CacheRead: series(5, 28, 1e9)},
		},
	}
}

type fakeSession struct {
	title, repo, branch, model string
	ctx                        int64
	active                     bool
	agents                     []string
}

var fakeSessions = []fakeSession{
	{"Add the checkout page", "acme-demo/web-shop", "feat/checkout", "claude-opus-5-5", 120_000, true, []string{"reviewer", "tester"}},
	{"Rate-limit the public API", "acme-demo/api-gateway", "feat/rate-limit", "claude-sonnet-5-5", 64_000, true, []string{"explorer"}},
	{"Fix the notebook export", "example-labs/notebook", "fix/export", "claude-sonnet-5-5", 91_000, true, nil},
	{"Rewrite the install guide", "example-labs/docs-site", "docs/install", "claude-haiku-5", 22_000, false, nil},
	{"Triage open issues", "acme-demo/web-shop", "main", "claude-sonnet-5-5", 15_000, false, nil},
}

func sessionID(i int) string { return fmt.Sprintf("d3m0%04x-0000-4000-8000-00000000%04x", i+1, i+1) }

// Snapshot is the invented picture of the agent network at Epoch: five
// sessions in two organisations, their sub-agents, leases and one tell.
func Snapshot(labColor, personalColor string) model.Snapshot {
	s := model.Snapshot{
		Version: model.Version, At: Epoch, ActiveFor: "10m0s", Horizon: "48h0m0s",
		Usage: Usage(labColor, personalColor), ContextWindow: 200_000,
		Stats: model.Stats{TranscriptsSeen: 5, TranscriptsRead: 5, LeasesRead: 4, TellsRead: 1, SubAgentsRead: 3, SessionRecords: 5},
	}
	for i, f := range fakeSessions {
		id := sessionID(i)
		age := time.Duration(i+1) * 3 * time.Minute
		if !f.active {
			age = time.Duration(i) * 40 * time.Minute
		}
		se := model.Session{
			ID: id, Short: model.ShortID(id), Title: f.title, Cwd: "/work/" + f.repo, CwdRepo: f.repo, Branch: f.branch,
			LastActivity: Epoch.Add(-age), Active: f.active, HasTranscript: true, Sent: i, MemAdds: 1,
			Model: f.model, Models: []model.ModelSeen{{ID: f.model, Last: Epoch.Add(-age)}}, Context: f.ctx,
		}
		for j, typ := range f.agents {
			se.Agents = append(se.Agents, model.Agent{
				ID: fmt.Sprintf("a%d%d", i, j), Type: typ, Description: "demo " + typ, Depth: 1,
				Model: f.model, Models: []model.ModelSeen{{ID: f.model, Last: Epoch.Add(-age)}},
				LastActivity: Epoch.Add(-age), Active: f.active, Context: f.ctx / 3,
			})
		}
		s.Sessions = append(s.Sessions, se)
		if f.active {
			s.Leases = append(s.Leases, model.Lease{
				Resource: f.repo, Repo: f.repo, Kind: model.KindRepo, Owner: id, PID: 4000 + i,
				Note: "demo: " + f.title, Acquired: Epoch.Add(-age), Expires: Epoch.Add(40 * time.Minute),
			})
		}
	}
	s.Tells = []model.Tell{{
		Target: model.ShortID(sessionID(1)), TargetKind: model.TargetSession, From: sessionID(0),
		Text: "demo: the checkout page needs the new rate limit", At: Epoch.Add(-2 * time.Minute),
	}}
	return s
}
