package collect

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
)

const sidCtx = "eeeeeeee-9999-4999-8999-999999999999"

func ctxOf(t *testing.T, snap model.Snapshot) model.Session {
	t.Helper()
	for _, s := range snap.Sessions {
		if s.ID == sidCtx {
			return s
		}
	}
	t.Fatal("the context session is not listed")
	return model.Session{}
}

func usage(in, cr, cc int64, id string) string {
	return `{"type":"assistant","timestamp":"2026-11-18T11:59:00Z","message":{"id":"` + id + `","model":"claude-opus-5-5","usage":{"input_tokens":` +
		itoa(int(in)) + `,"cache_read_input_tokens":` + itoa(int(cr)) + `,"cache_creation_input_tokens":` + itoa(int(cc)) + `,"output_tokens":9}}}`
}

// The context of a session is what its LAST call carried in: input, cache read
// and cache creation summed. Entries of one message id are one call, the last
// one says it; a synthetic entry and an entry with no usage change nothing.
func TestContextIsTheLastCallsInput(t *testing.T) {
	f := newFixture(t)
	proj := filepath.Join(f.claude, "projects", "-proj")
	f.transcript(proj, sidCtx, f.now.Add(-time.Minute),
		usage(10, 100000, 5000, "m1"),
		usage(10, 150000, 2000, "m2"),
		usage(10, 150000, 2000, "m2"), // the same call again, as its second entry
		`{"type":"assistant","timestamp":"2026-11-18T11:59:30Z","message":{"id":"m3","model":"<synthetic>","usage":{"input_tokens":0}}}`,
		`{"type":"assistant","timestamp":"2026-11-18T11:59:40Z","message":{"id":"m4","model":"claude-opus-5-5"}}`,
		`{"type":"user","message":"a string, not an object"}`,
	)
	c := New(f.options())
	if got := ctxOf(t, c.Snapshot()).Context; got != 10+150000+2000 {
		t.Fatalf("context %d, want the last call's 152010", got)
	}
	// Incremental: appended lines move it, and nothing is read again.
	fh, err := os.OpenFile(filepath.Join(proj, sidCtx+".jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	fh.WriteString(usage(1, 200000, 0, "m5") + "\n")
	fh.Close()
	if got := ctxOf(t, c.Snapshot()).Context; got != 200001 {
		t.Fatalf("after an append: %d", got)
	}
}

func TestContextOfASubAgentAndOfAHostileLine(t *testing.T) {
	f := newFixture(t)
	proj := filepath.Join(f.claude, "projects", "-proj")
	f.transcript(proj, sidCtx, f.now.Add(-time.Minute), usage(1, 2, 3, "m1"))
	sub := filepath.Join(proj, sidCtx, "subagents")
	writeFile(t, filepath.Join(sub, "agent-c1.meta.json"), `{"agentType":"Explore"}`)
	writeFile(t, filepath.Join(sub, "agent-c1.jsonl"), usage(40000, 0, 0, "s1")+"\n")
	writeFile(t, filepath.Join(sub, "agent-c2.meta.json"), `{"agentType":"Explore"}`)
	writeFile(t, filepath.Join(sub, "agent-c2.jsonl"),
		`{"type":"assistant","message":{"model":"claude-opus-5-5","usage":{"input_tokens":9223372036854775807,"cache_read_input_tokens":9223372036854775807,"cache_creation_input_tokens":-5}}}`+"\n")
	s := ctxOf(t, New(f.options()).Snapshot())
	got := map[string]int64{}
	for _, a := range s.Agents {
		got[a.ID] = a.Context
	}
	if got["c1"] != 40000 {
		t.Fatalf("sub-agent context %v", got)
	}
	if got["c2"] != 2*maxContext {
		t.Fatalf("a hostile line is bounded per field, not overflowed: %d", got["c2"])
	}
}

func TestCompactWindowFallbackChain(t *testing.T) {
	dir := t.TempDir()
	env := func(v string) func(string) string {
		return func(k string) string {
			if k != "CLAUDE_CODE_AUTO_COMPACT_WINDOW" {
				t.Fatalf("read %q", k)
			}
			return v
		}
	}
	settings := func(body string) {
		t.Helper()
		writeFile(t, filepath.Join(dir, "settings.json"), body)
	}
	// Nothing at all: the default.
	if got := CompactWindow(dir, env("")); got != 1_000_000 {
		t.Fatalf("default %d", got)
	}
	// The environment alone.
	if got := CompactWindow(dir, env("450000")); got != 450000 {
		t.Fatalf("env %d", got)
	}
	// The settings win over the environment.
	settings(`{"autoCompactWindow":300000}`)
	if got := CompactWindow(dir, env("450000")); got != 300000 {
		t.Fatalf("settings %d", got)
	}
	// A settings value that is no window falls through to the next source.
	for _, bad := range []string{`{"autoCompactWindow":0}`, `{"autoCompactWindow":-4}`, `{"autoCompactWindow":"abc"}`,
		`{"autoCompactWindow":2.5}`, `{"autoCompactWindow":true}`, `{"autoCompactWindow":99999999999999999999}`, `{}`, `not json`} {
		settings(bad)
		if got := CompactWindow(dir, env("450000")); got != 450000 {
			t.Fatalf("%s: %d, want the environment's", bad, got)
		}
		if got := CompactWindow(dir, env("zero")); got != 1_000_000 {
			t.Fatalf("%s with a bad environment: %d, want the default", bad, got)
		}
	}
	// A window given as a string of digits counts.
	settings(`{"autoCompactWindow":"250000"}`)
	if got := CompactWindow(dir, env("")); got != 250000 {
		t.Fatalf("string %d", got)
	}
}

func TestSnapshotCarriesTheWindow(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(f.claude, "settings.json"), `{"autoCompactWindow":300000}`)
	if got := New(f.options()).Snapshot().ContextWindow; got != 300000 {
		t.Fatalf("window %d", got)
	}
}
