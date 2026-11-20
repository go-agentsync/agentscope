package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/guard"
)

// refusal is the transcript entry of a tool result that a gate refused, as Claude
// Code writes it: a user entry holding a tool_result with is_error.
func refusal(at time.Time, id, text string) string {
	b, _ := json.Marshal(text)
	return fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":%s,"is_error":true}]},"toolUseResult":%s}`+"\n",
		at.UTC().Format(time.RFC3339Nano), id, b, b)
}

// refusalParts is the same entry with the text as a list of parts.
func refusalParts(at time.Time, id, text string) string {
	b, _ := json.Marshal(text)
	return fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":[{"type":"text","text":%s}],"is_error":true}]}}`+"\n",
		at.UTC().Format(time.RFC3339Nano), id, b)
}

func refs(s *Scanner) map[string]int64 {
	out := map[string]int64{}
	for _, c := range s.Refusals() {
		out[c.Gate] += c.N
	}
	return out
}

const (
	bashRefusal  = "PreToolUse:Bash hook error: Refused: it does the work in the shell instead of invoking a tool: a pipeline.\n\nit matched `ls | head`"
	stampRefusal = "PreToolUse:Read hook error: This turn has written no text yet, and CLAUDE.md's first rule is to START every reply"
	oddRefusal   = "PreToolUse:Bash hook error: a gate nobody has written a rule for"
)

func TestScannerCountsRefusalsPerGateIncludingSubagentsAndUnknownOnes(t *testing.T) {
	root := t.TempDir()
	at := now.Add(-time.Hour)
	write(t, root, "p/a.jsonl",
		refusal(at, "t1", bashRefusal),
		refusal(at, "t2", bashRefusal),
		refusalParts(at, "t3", stampRefusal),
		refusal(at, "t4", oddRefusal),
		entry(at, "m1", "claude-opus-5-5", 5, 0), // a usage line in the same file still counts
	)
	write(t, root, "p/a/subagents/agent-1.jsonl", refusal(at, "t5", bashRefusal))
	s := NewScanner(filepath.Join(root, "projects"), "")
	if s.Scanned() {
		t.Fatal("scanned before any pass")
	}
	s.Refresh(now)
	got := refs(s)
	if got[guard.GuardBash] != 3 || got[guard.Replystamp] != 1 || got[guard.Other] != 1 || len(got) != 3 {
		t.Fatalf("%v", got)
	}
	if !s.Scanned() || total(s, 0, "").Out != 5 {
		t.Fatalf("scanned %v, %d tokens", s.Scanned(), total(s, 0, "").Out)
	}
	// Oldest first, hour by hour, then by gate.
	rs := s.Refusals()
	for i := 1; i < len(rs); i++ {
		if rs[i-1].Hour > rs[i].Hour || (rs[i-1].Hour == rs[i].Hour && rs[i-1].Gate >= rs[i].Gate) {
			t.Fatalf("not ordered: %v", rs)
		}
	}
	if rs[0].Hour%3600 != 0 {
		t.Fatalf("hour %d", rs[0].Hour)
	}
}

// The hour decides the order before the gate does, whichever gate sorts first.
func TestRefusalsAreOrderedByHourBeforeGate(t *testing.T) {
	root := t.TempDir()
	older, newer := now.Add(-5*time.Hour), now.Add(-2*time.Hour)
	write(t, root, "p/a.jsonl",
		refusal(older, "t1", bashRefusal), refusal(newer, "t2", stampRefusal),
		refusal(older, "t3", stampRefusal), refusal(newer, "t4", bashRefusal))
	s := NewScanner(filepath.Join(root, "projects"), "")
	s.Refresh(now)
	rs := s.Refusals()
	if len(rs) != 4 || rs[0].Hour != rs[1].Hour || rs[2].Hour != rs[3].Hour || rs[1].Hour >= rs[2].Hour {
		t.Fatalf("%v", rs)
	}
}

func TestScannerDoesNotCountWhatMerelyMentionsAHook(t *testing.T) {
	root := t.TempDir()
	at := now.Add(-time.Hour)
	notError := strings.Replace(refusal(at, "t1", bashRefusal), `"is_error":true`, `"is_error":false`, 1)
	assistant := strings.Replace(refusal(at, "t2", bashRefusal), `"type":"user"`, `"type":"assistant"`, 1)
	write(t, root, "p/a.jsonl",
		notError,
		assistant,
		refusal(at, "t3", "1\tthe file says: PreToolUse:Bash hook error: Refused: x"), // a file that quotes one
		refusal(at, "t4", `{"hookSpecificOutput":{"permissionDecision":"allow"}}`),
		`{"type":"user","timestamp":"`+at.UTC().Format(time.RFC3339Nano)+`","message":{"content":"a hook error: string, tool_result"}}`+"\n", // content is a plain string
		`{"type":"user","timestamp":"nonsense","message":{"content":[{"type":"tool_result","content":"PreToolUse:Bash hook error: Refused","is_error":true}]}}`+"\n",
		`{"type":"user","timestamp":"`+at.UTC().Format(time.RFC3339Nano)+`","message":{"content":[{"type":"text","text":"tool_result hook error"},{"type":"tool_result","content":[1,2],"is_error":true}]}}`+"\n",
		`{"type":"user","message":{"content":[{"type":"tool_result","content":"hook error","is_error":true}]}`+"\n", // torn JSON
	)
	s := NewScanner(filepath.Join(root, "projects"), "")
	s.Refresh(now)
	if got := refs(s); len(got) != 0 {
		t.Fatalf("%v", got)
	}
}

func TestScannerKeepsRefusalsForTwelveWeeksAndDropsOlderOnes(t *testing.T) {
	root := t.TempDir()
	recent := now.Add(-80 * 24 * time.Hour) // past Keep, inside KeepRefusals: kept
	old := now.Add(-100 * 24 * time.Hour)   // past KeepRefusals: not kept
	p := write(t, root, "p/a.jsonl", refusal(old, "t0", bashRefusal), refusal(recent, "t1", stampRefusal), refusal(now.Add(-time.Hour), "t2", bashRefusal), entry(recent, "m1", "claude-opus-5-5", 7, 0))
	if err := os.Chtimes(p, now, now); err != nil {
		t.Fatal(err)
	}
	s := NewScanner(filepath.Join(root, "projects"), "")
	s.Refresh(now)
	got := refs(s)
	if got[guard.GuardBash] != 1 || got[guard.Replystamp] != 1 {
		t.Fatalf("%v", got)
	}
	if total(s, 0, "").Out != 0 {
		t.Fatal("a token line past Keep was counted because its file was read for refusals")
	}
	// Later, the count of what is past the window is dropped.
	s.Refresh(now.Add(30 * 24 * time.Hour))
	if got := refs(s); got[guard.Replystamp] != 0 {
		t.Fatalf("%v", got)
	}
}

func TestRefusalsSurviveARestartAndAreNotCountedTwice(t *testing.T) {
	root, cache := t.TempDir(), filepath.Join(t.TempDir(), "usage.json")
	at := now.Add(-time.Hour)
	p := write(t, root, "p/a.jsonl", refusal(at, "t1", bashRefusal), refusal(at, "t2", stampRefusal))
	s := NewScanner(filepath.Join(root, "projects"), cache)
	s.Refresh(now)
	s.Refresh(now) // nothing new
	if got := refs(s); got[guard.GuardBash] != 1 || got[guard.Replystamp] != 1 {
		t.Fatalf("%v", got)
	}
	s2 := NewScanner(filepath.Join(root, "projects"), cache)
	if got := refs(s2); got[guard.GuardBash] != 1 {
		t.Fatalf("cache not loaded: %v", got)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(refusal(at, "t3", bashRefusal))
	f.Close()
	s2.Refresh(now)
	if got := refs(s2); got[guard.GuardBash] != 2 || got[guard.Replystamp] != 1 {
		t.Fatalf("only the appended entry may be read after a restart: %v", got)
	}
	// A count that names no gate, or is not positive, is not trusted.
	if err := os.WriteFile(cache, []byte(`{"v":3,"refs":[{"h":3600,"g":"","n":4},{"h":3600,"g":"x","n":0},{"h":3600,"g":"guard-bash","n":2}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := refs(NewScanner(filepath.Join(root, "projects"), cache)); len(got) != 1 || got["guard-bash"] != 2 {
		t.Fatalf("%v", got)
	}
}
