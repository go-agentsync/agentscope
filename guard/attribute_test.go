package guard

import (
	"reflect"
	"strings"
	"testing"
)

// Refusals as the transcripts hold them: the text of an error tool result, shaped
// by a week of real ones (the commands they quote are shortened).
var samples = []struct {
	name, text, gate string
}{
	{"shell pipeline", "PreToolUse:Bash hook error: Refused: it does the work in the shell instead of invoking a tool: a pipeline.\n\nit matched `ls ~/.local/bin | head -200`\n\nredirect and read the file", GuardBash},
	{"test filter", "PreToolUse:Bash hook error: Refused: it filters a test run and does not check the filter: a `-run` whose match nobody verifies.\n\nit matched `go test -run 'TestX' .`\n\nUse instead: gotest", GuardBash},
	{"discard", "PreToolUse:Bash hook error: Refused: overwrites those paths from a commit, discarding whatever the working tree held\n\nIt matched git checkout … -- …", GuardBash},
	{"volatile redirect", "PreToolUse:Bash hook error: Refused (volatile redirect): /private/tmp/x/pre.txt — a background command's output written to a volatile directory is lost", GuardBash},
	{"a quoted gate name does not move a guard-bash refusal", "PreToolUse:Bash hook error: Refused: it does the work in the shell instead of invoking a tool: a pipeline.\n\nit matched `mutsweep -hook | modelgate ctxgate replystamp`", GuardBash},
	{"stamp, nothing written", "PreToolUse:Read hook error: This turn has written no text yet, and CLAUDE.md's first rule is to START every reply with the local date and time.", Replystamp},
	{"stamp, stale", "PreToolUse:Bash hook error: This turn's first text carries a stale stamp (7.9 hours old).", Replystamp},
	{"stamp, not at the start", "PreToolUse:Agent hook error: This turn's first text does not START with the local date and time: \"Let me look.\"", Replystamp},
	{"stamp, deny JSON", `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"This turn's first text does not START with the local date and time"}}`, Replystamp},
	{"modtidy", "PreToolUse:Bash hook error: modtidy refuses this push (go mod tidy at the root does not enter a nested module).\nmodule \".\" is untidy", Modtidy},
	{"mutsweep", "PreToolUse:Bash hook error: [~/.local/bin/mutsweep -hook]: ⛔ mutsweep gate: this push sends Go source whose refusals no sweep has covered.\n\n  durability.go  never swept as committed (5 refusals)", Mutsweep},
	{"modelgate", "PreToolUse:Agent hook error: modelgate refuses this Agent call: class implement needs sonnet", Modelgate},
	{"ctxgate", "PreToolUse:SendMessage hook error: ctxgate refuses this SendMessage: agent a78cea3387 is at 91% of its context", Ctxgate},
	{"recur", "PreToolUse:Bash hook error: 1 of 238 recorded recurrence(s) have no gate, and this command publishes:\n  a-guard recorded a recurrence and has no gate", Recur},
	{"recur, new recurrence", "PreToolUse:Bash hook error: 2 of 262 recorded recurrence(s) have no gate, and this command publishes:\n  defect-lessons-index recorded a NEW recurrence since it was accounted for", Recur},
	{"agentsync, expired", "PreToolUse:Bash hook error: agentsync: no session holds go-embedded-ruby/ruby: this session's claim expired at 17:08:01.\nThis command publishes into it.", Agentsync},
	{"agentsync, held by another", "PreToolUse:Bash hook error: agentsync: go-fleettools/fleettools is claimed by another session, 4abf99a9, until 23:00:19.", Agentsync},
	{"by-hand GitHub write", "PreToolUse:Bash hook error: Refused: it would write to the GitHub API by hand, and no fleet program does that act yet.", Ghbyhand},
	{"by-hand run watch", "PreToolUse:Bash hook error: Refused: it would wait for a workflow run by hand, and the fleet has a program for that act.\n\nit matched gh run watch 4379", Ghbyhand},
	{"prbody", "PreToolUse:Bash hook error: Refused: it opens a pull request from `b.txt` without `prbody b.txt --repo go-fde/.github` immediately before it, joined by `&&`.", Prbody},
	{"prbody, check and write in one line", "PreToolUse:Bash hook error: Refused: it runs a check and the write that check decides, in ONE command line: `prbody` cannot stop `ghpr go-filesystems/hfsplus 50` from the same breath.", Prbody},
	{"paritycheck", "PreToolUse:Bash hook error: Refused: it opens a pull request with a body `paritycheck text b.md` has not read.", Paritychk},
	{"guard-bash names the wrapper in its own words", "PreToolUse:Bash hook error: Refused: ghbyhand would refuse this: use ghissue", Ghbyhand},
	{"quotaclaim", "PreToolUse:Bash hook error: quotaclaim: no account has room for this session", Quotaclaim},
	{"an unknown prefix goes to other gate", "PreToolUse:Bash hook error: gatekeeper-9000 says no: the moon is in the wrong house", Other},
	{"an unknown tool and an unknown event are still refusals", "PostToolUse:mcp__x__y hook error: something nobody wrote a rule for", Other},
}

func TestAttributionOfRealShapedRefusals(t *testing.T) {
	for _, s := range samples {
		body, ok := Refusal(s.text, true)
		if !ok {
			t.Errorf("%s: not seen as a refusal", s.name)
			continue
		}
		if got := Attribute(body); got != s.gate {
			t.Errorf("%s: attributed to %q, want %q", s.name, got, s.gate)
		}
	}
}

func TestOnlyAnErrorThatStartsLikeARefusalIsOne(t *testing.T) {
	for name, c := range map[string]struct {
		text    string
		isError bool
	}{
		"not marked as an error":                   {"PreToolUse:Bash hook error: Refused: a pipeline", false},
		"a file that mentions a hook":              {"1\tnote: the hook error: Refused shows up in this file", true},
		"a log whose line holds the words":         {"--- FAIL: TestHook\n    PreToolUse:Bash hook error: Refused", true},
		"a prefix with a space is not a hook":      {"some words PreToolUse hook error: Refused", true},
		"deny JSON in a result that is no error":   {`{"hookSpecificOutput":{"permissionDecision":"deny"}}`, false},
		"allow JSON is not a refusal":              {`{"hookSpecificOutput":{"permissionDecision":"allow"}}`, true},
		"the marker too far from the start":        {strings.Repeat("x", 70) + " hook error: Refused", true},
		"empty":                                    {"", true},
		"the marker at the very start has no name": {" hook error: Refused", true},
	} {
		if _, ok := Refusal(c.text, c.isError); ok {
			t.Errorf("%s: counted as a refusal", name)
		}
	}
	if body, ok := Refusal("  \nPreToolUse:Bash hook error: Refused: x\n", true); !ok || body != "Refused: x" {
		t.Errorf("surrounding blanks: %q %v", body, ok)
	}
}

func TestAHookNameWithAnEmptyEventIsNotAPrefix(t *testing.T) {
	if validPrefix(":Bash") || validPrefix("") || validPrefix("Pre Tool") || validPrefix("Pre:Ba sh") {
		t.Fatal("an invalid prefix was accepted")
	}
	if !validPrefix("PreToolUse") || !validPrefix("PreToolUse:mcp__a__b") {
		t.Fatal("a valid prefix was refused")
	}
}

func TestAttributeLooksOnlyAtTheHead(t *testing.T) {
	long := "Refused: it does the work in the shell: a pipeline. " + strings.Repeat("x", maxHead) + " prbody"
	if got := Attribute(long); got != GuardBash {
		t.Fatalf("%q", got)
	}
}

func TestGatesListsEveryAttributionAndOtherLast(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range samples {
		seen[s.gate] = true
	}
	for _, g := range Gates {
		if !seen[g] {
			t.Errorf("gate %q has no sample", g)
		}
	}
	if Gates[len(Gates)-1] != Other {
		t.Fatal("other gate is not last")
	}
}

func TestNamedFindsTheGatesALedgerTextNames(t *testing.T) {
	got := Named("replystamp --pretool (PreToolUse) and --hook (Stop); also mutsweep -hook, ModTidy, modelgate, ctxgate, guard-bash, ghbyhand, paritycheck, prbody, quotaclaim, agentsync gate, recur --hook")
	want := []string{GuardBash, Replystamp, Agentsync, Recur, Mutsweep, Modtidy, Modelgate, Ctxgate, Ghbyhand, Paritychk, Prbody, Quotaclaim}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
	// A test, a CI step or a bare tool name that is not a hook counts nothing.
	if got := Named("TestR2RCPUDependentFloor in go-fft/fft, and `recur --memory x --accept y`, agentsync claim"); got != nil {
		t.Fatalf("%v", got)
	}
}
