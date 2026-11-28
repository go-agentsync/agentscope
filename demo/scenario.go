package demo

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-agentsync/agentscope/ui"
)

// Env is what a step may touch: the screen and its view-model, the only
// door the live UI has too.
type Env struct {
	Scene *ui.Scene
	VM    *ui.ViewModel
}

// Step is one beat of a scenario: Do changes the view-model, then the screen is
// recorded and held for Hold.
type Step struct {
	Hold time.Duration
	Do   func(*Env)
}

// Scenario is a named, ordered list of steps.
type Scenario struct {
	Name, Title string
	Steps       func() []Step
}

const beat = 1500 * time.Millisecond

func apply(lab, personal string) func(*Env) {
	// A snapshot only arrives on a live connection, so the status pill says so.
	return func(e *Env) {
		e.VM.SetConn("connected", "")
		e.VM.Apply(Snapshot(lab, personal))
	}
}

func setPrefs(theme, lang int) func(*Env) {
	return func(e *Env) {
		e.VM.Prefs.ThemeSel.Set(theme)
		e.VM.Prefs.LangSel.Set(lang)
	}
}

// colour walks the Colour dialog of the accounts table as its owner does: select
// the row, open the dialog, pick, confirm. The server's answer is the next
// snapshot, which is what Send does here.
func colour(row int, r, g, b uint8, held *[2]string) []Step {
	pick := ui.PickRGB(r, g, b)
	return []Step{
		{beat, func(e *Env) { e.VM.CapSel.Set(row) }},
		{beat, func(e *Env) { e.VM.Color.Begin.Execute() }},
		{beat, func(e *Env) { e.VM.Color.Pick.Set(pick) }},
		{beat, func(e *Env) {
			e.VM.Color.Send = func(_, c string) {
				held[row] = c
				e.VM.Apply(Snapshot(held[0], held[1]))
				e.VM.Color.Done(nil)
			}
			e.VM.Color.Confirm.Execute()
		}},
	}
}

var scenarios = []Scenario{
	{"overview", "agentscope overview: the graph, the side panel, the quota", func() []Step {
		return []Step{
			{beat, func(e *Env) { setPrefs(1, 1)(e) }},
			{beat, apply("", "")},
			{beat, func(e *Env) { e.VM.PanelOpen.Set(true) }},
			{beat, func(e *Env) { e.VM.TableSel.Set(0) }},
			{beat, func(e *Env) { e.VM.Tab.Set(ui.TabQuota) }},
			{beat, func(e *Env) { e.VM.Span.Set(ui.SpanShort) }},
			{beat, func(e *Env) { e.VM.Account.Set(1) }},
		}
	}},
	{"colors", "agentscope account colours: the Colour dialog, then each account in its own colour", func() []Step {
		held := new([2]string) // the colours the owner has given so far
		steps := []Step{
			{beat, setPrefs(1, 1)},
			{beat, apply("", "")},
			{beat, func(e *Env) { e.VM.Tab.Set(ui.TabQuota) }},
		}
		steps = append(steps, colour(0, 0xD5, 0x5E, 0x00, held)...)
		steps = append(steps, colour(1, 0x00, 0x72, 0xB2, held)...)
		return append(steps, Step{2 * beat, func(e *Env) { e.VM.CapSel.Set(-1) }})
	}},
	{"theme-lang", "agentscope theme and language: Light to Dark, English to French", func() []Step {
		return []Step{
			{beat, setPrefs(1, 1)},
			{beat, apply("#d55e00", "#0072b2")},
			{beat, func(e *Env) { e.VM.Tab.Set(ui.TabQuota) }},
			{beat, setPrefs(2, 1)}, // Light -> Dark
			{beat, setPrefs(2, 2)}, // en -> fr
			{beat, func(e *Env) { e.VM.Tab.Set(ui.TabGraph); e.VM.PanelOpen.Set(true) }},
			{2 * beat, func(e *Env) { e.VM.TableSel.Set(1) }},
		}
	}},
}

// Names are the scenarios, sorted.
func Names() []string {
	out := make([]string, 0, len(scenarios))
	for _, s := range scenarios {
		out = append(out, s.Name)
	}
	sort.Strings(out)
	return out
}

// Lookup returns the scenario called name, or an error naming the valid ones.
func Lookup(name string) (Scenario, error) {
	for _, s := range scenarios {
		if s.Name == name {
			return s, nil
		}
	}
	return Scenario{}, fmt.Errorf("unknown scenario %q (have %s)", truncate(name, 40), strings.Join(Names(), ", "))
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}

// Size bounds: a capture is a picture of a window, not an allocation attack.
const (
	MinSize = 320
	MaxSize = 4096
)

// ParseSize reads "WIDTHxHEIGHT" within [MinSize, MaxSize].
func ParseSize(s string) (w, h int, err error) {
	ws, hs, _ := strings.Cut(s, "x") // no "x": hs is "" and Atoi refuses it
	w, err = strconv.Atoi(ws)
	if err == nil {
		h, err = strconv.Atoi(hs)
	}
	if err != nil || w < MinSize || w > MaxSize || h < MinSize || h > MaxSize {
		return 0, 0, fmt.Errorf("size %q: want WIDTHxHEIGHT, each %d to %d", truncate(s, 40), MinSize, MaxSize)
	}
	return w, h, nil
}
