// Package guard is the Guardrails tab's data: which gates refused what, and
// whether the lessons that became gates stopped recurring.
//
// A gate refusal is a tool result the hook runner turned into an error: its text
// starts with "<Event>:<Tool> hook error: " (or is the hook's own deny JSON). The
// attribution table below was built from the real refusals of a week of
// transcripts; a refusal that matches no row is "other gate", counted and shown,
// never dropped.
package guard

import (
	"strings"
)

// The gates a refusal is attributed to. Other holds what no rule recognises.
const (
	GuardBash  = "guard-bash"
	Replystamp = "replystamp"
	Agentsync  = "agentsync gate"
	Recur      = "recur"
	Mutsweep   = "mutsweep"
	Modtidy    = "modtidy"
	Modelgate  = "modelgate"
	Ctxgate    = "ctxgate"
	Ghbyhand   = "ghbyhand"
	Paritychk  = "paritycheck"
	Prbody     = "prbody"
	Quotaclaim = "quotaclaim"
	Other      = "other gate"
)

// Gates lists every gate in the stack order of the chart; Other is last.
var Gates = []string{GuardBash, Replystamp, Agentsync, Recur, Mutsweep, Modtidy, Modelgate, Ctxgate, Ghbyhand, Paritychk, Prbody, Quotaclaim, Other}

// hookMark is what a refusal's text carries after its prefix.
const hookMark = " hook error: "

// maxHead bounds how much of a result is looked at to tell its gate: the gate's
// own words come first, and the rest is the command it quotes.
const maxHead = 600

// Refusal reports whether a tool result's text is a gate refusal, and its body: the
// text after the hook prefix. Only a result the runner marked as an error, whose
// text STARTS like a refusal, is one: a file or a log that merely mentions a hook
// is not.
func Refusal(text string, isError bool) (body string, ok bool) {
	if !isError {
		return "", false
	}
	text = strings.TrimSpace(text)
	if i := strings.Index(text, hookMark); i > 0 && i <= 64 && validPrefix(text[:i]) {
		return text[i+len(hookMark):], true
	}
	if strings.HasPrefix(text, `{"hookSpecificOutput"`) && strings.Contains(text, `"permissionDecision":"deny"`) {
		return text, true
	}
	return "", false
}

// validPrefix is "PreToolUse" or "PreToolUse:Bash": letters, then an optional
// tool name (which may be an mcp__ one).
func validPrefix(p string) bool {
	event, tool, _ := strings.Cut(p, ":")
	if event == "" || strings.ContainsAny(event, " \n\t") {
		return false
	}
	return !strings.ContainsAny(tool, " \n\t")
}

// Attribute names the gate that wrote a refusal body.
func Attribute(body string) string {
	head := body
	if len(head) > maxHead {
		head = head[:maxHead]
	}
	has := func(s string) bool { return strings.Contains(head, s) }
	switch {
	case strings.HasPrefix(head, "Refused"):
		// guard-bash's words come first; what follows quotes the command it refused,
		// which can name any other gate.
		return refused(head)
	case has("modelgate refuses"):
		return Modelgate
	case has("ctxgate refuses"):
		return Ctxgate
	case has("modtidy refuses") || strings.HasPrefix(head, "modtidy"):
		return Modtidy
	case has("mutsweep gate") || has("mutsweep -hook"):
		return Mutsweep
	case has("recorded recurrence(s) have no gate") || has("recorded a NEW recurrence"):
		return Recur
	case strings.HasPrefix(head, "agentsync:"):
		return Agentsync
	case has("This turn's first text") || has("This turn has written no text yet"):
		return Replystamp
	case has("quotaclaim"):
		return Quotaclaim
	}
	return Other
}

// refused splits guard-bash's refusals: it also fronts the fleet's wrappers (the
// PR body check, the parity check, the by-hand GitHub writes), which are told
// apart by what the refusal says it wanted instead.
func refused(head string) string {
	for _, m := range []string{"it matched", "It matched"} {
		if i := strings.Index(head, m); i >= 0 {
			head = head[:i] // the quoted command is not the refusal's own words
		}
	}
	has := func(s string) bool { return strings.Contains(head, s) }
	switch {
	case has("prbody"):
		return Prbody
	case has("paritycheck"):
		return Paritychk
	case has("by hand") && (has("GitHub") || has("the fleet has a program") || has("fleet program")):
		return Ghbyhand
	case has("ghbyhand"):
		return Ghbyhand
	}
	return GuardBash
}

// Named lists the gates a ledger text names (a lesson's gate is written in words:
// "replystamp --pretool", "mutsweep -hook"), so a lesson's refusals can be found.
// "recur" and "agentsync" alone name too many other things to count: they need
// their flag, "recur --hook" or "agentsync gate".
func Named(text string) []string {
	low := strings.ToLower(text)
	var out []string
	add := func(g string, needles ...string) {
		for _, n := range needles {
			if strings.Contains(low, n) {
				out = append(out, g)
				return
			}
		}
	}
	add(GuardBash, "guard-bash", "guardbash")
	add(Replystamp, "replystamp")
	add(Agentsync, "agentsync gate")
	add(Recur, "recur --hook")
	add(Mutsweep, "mutsweep")
	add(Modtidy, "modtidy")
	add(Modelgate, "modelgate")
	add(Ctxgate, "ctxgate")
	add(Ghbyhand, "ghbyhand")
	add(Paritychk, "paritycheck")
	add(Prbody, "prbody")
	add(Quotaclaim, "quotaclaim")
	return out
}
