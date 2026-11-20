package usage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/guard"
	"github.com/go-agentsync/agentscope/model"
)

type fixedSnap struct{}

func (fixedSnap) Snapshot() model.Snapshot { return model.Snapshot{Version: 1} }

const recurAnswer = `{"lessons":[{"name":"l","recurrences":[{"date":"2026-11-19","line":"again"}],"ledger":{"decision":"gate","text":"mutsweep -hook","writtenAt":"2026-11-01T10:00:00Z"}}],"totals":{"lessons":1}}`

func TestEngineBuildsTheGuardrailsViewFromRecurAndTheTranscripts(t *testing.T) {
	root := t.TempDir()
	write(t, root, "p/a.jsonl", refusal(now.Add(-time.Hour), "t1", bashRefusal))
	e := NewEngine(root, t.TempDir(), t.TempDir(), func(context.Context) ([]byte, error) { return nil, errors.New("no aiquota") })
	if e.Guardrails() != nil {
		t.Fatal("a view before the first pass")
	}
	e.SetRecur(&guard.Source{Dir: "/m", Run: func(context.Context, string) ([]byte, error) { return []byte(recurAnswer), nil }})
	e.Step(context.Background(), now)
	g := e.Guardrails()
	if g == nil || !g.RecurMeasured || !g.RefusalsMeasured || g.Lessons != 1 || g.Leaking != 1 || g.ThisWeek != 1 {
		t.Fatalf("%+v", g)
	}
	// The snapshot carries it beside the usage.
	snap := Source{Inner: fixedSnap{}, Eng: e}.Snapshot()
	if snap.Guardrails != g || snap.Usage == nil {
		t.Fatalf("%+v", snap)
	}
}

func TestEngineSaysNotMeasuredWhenRecurFailsOrIsNotConfigured(t *testing.T) {
	root := t.TempDir()
	e := NewEngine(root, t.TempDir(), t.TempDir(), func(context.Context) ([]byte, error) { return nil, errors.New("no aiquota") })
	e.Step(context.Background(), now)
	if g := e.Guardrails(); g == nil || g.RecurMeasured || g.RecurErr == "" || len(g.Rows) != 0 {
		t.Fatalf("not configured: %+v", g)
	}
	e.SetRecur(&guard.Source{Dir: "/m", Run: func(context.Context, string) ([]byte, error) { return nil, errors.New("recur: boom") }})
	e.Step(context.Background(), now.Add(time.Minute))
	if g := e.Guardrails(); g.RecurMeasured || g.RecurErr != "recur: boom" || g.Untriaged != 0 || len(g.Rows) != 0 {
		t.Fatalf("failing: %+v", g)
	}
	if !e.Guardrails().RefusalsMeasured {
		t.Fatal("the transcripts were scanned: that source is measured whatever recur says")
	}
}
