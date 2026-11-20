package ui

import (
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/toolkit"
)

func TestTokenCountAndContextText(t *testing.T) {
	for n, want := range map[int64]string{0: "0", 999: "999", 1000: "1k", 1499: "1k", 1500: "2k", 212_300: "212k", 300_000: "300k", 999_999: "1000k",
		1_000_000: "1M", 1_049_999: "1M", 1_050_000: "1.1M", 1_500_000: "1.5M", 2_960_000: "3M"} {
		if got := tokenCount(n); got != want {
			t.Errorf("tokenCount(%d) = %q, want %q", n, got, want)
		}
	}
	if contextText(212_000, 300_000) != "212k / 300k" || contextText(0, 300_000) != "- / 300k" || contextText(-1, 1_000_000) != "- / 1M" {
		t.Fatal("contextText")
	}
	g := ctxOf("x", 150_000, 300_000)
	if g.Ratio != 0.5 || g.Band != model.BandFair || g.Text != "150k / 300k" || g.Cells()[0] != "x" || g.Cells()[1] != "150k / 300k" {
		t.Fatalf("%+v", g)
	}
}

// The five bands have five colours, told apart, in the light and in the dark
// theme, and the two greens differ by lightness (dark below 40 %, light from 40
// to 60 %).
func TestPressureColoursAreFiveAndDistinctInBothThemes(t *testing.T) {
	lum := func(c toolkit.RGBA) float64 { return 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B) }
	for _, dark := range []bool{false, true} {
		seen := map[toolkit.RGBA]bool{}
		for b := 0; b < model.Bands; b++ {
			c := PressureColor(b, dark)
			if c.A != 0xFF || seen[c] {
				t.Fatalf("dark=%v band %d: %v repeats or is not opaque", dark, b, c)
			}
			seen[c] = true
		}
		if lum(PressureColor(model.BandCalm, dark)) >= lum(PressureColor(model.BandFair, dark)) {
			t.Fatalf("dark=%v: below 40 %% is the DARK green, 40-60 %% the LIGHT one", dark)
		}
		// Red is red, yellow is yellow, orange sits between.
		red, yel, org := PressureColor(model.BandCritical, dark), PressureColor(model.BandWarm, dark), PressureColor(model.BandHot, dark)
		if red.R <= red.G || yel.R < 0xC0 || yel.G < 0xB0 || org.R <= org.G || org.G <= red.G || org.G >= yel.G {
			t.Fatalf("dark=%v: red %v orange %v yellow %v", dark, red, org, yel)
		}
	}
	if PressureColor(-3, false) != PressureColor(0, false) || PressureColor(99, true) != PressureColor(model.Bands-1, true) {
		t.Fatal("a band out of range is held to the nearest")
	}
}

func ctxSnap() model.Snapshot {
	sn := snap()
	sn.ContextWindow = 300_000
	sn.Sessions[0].Context = 225_000 // 75 %: hot
	sn.Sessions[0].Agents[0].Context = 60_000
	sn.Sessions[1].Context = 90_000 // 30 %: calm
	return sn
}

// The Context column and the Details gauges carry the same ratio and band; a
// window the collector did not send is the default one.
func TestRowsAndDetailsCarryTheContext(t *testing.T) {
	vm := NewViewModel()
	vm.Apply(ctxSnap())
	a, b := vm.Rows.At(0), vm.Rows.At(1)
	if a.CtxText != "225k / 300k" || a.Ratio != 0.75 || a.Band != model.BandHot || b.Band != model.BandCalm || b.CtxText != "90k / 300k" {
		t.Fatalf("%+v %+v", a, b)
	}
	if c := vm.Rows.At(2); c.CtxText != "- / 300k" || c.Ratio != 0 {
		t.Fatalf("a session with no call: %+v", c)
	}
	// Selecting a session lists its gauge and each sub-agent's.
	vm.Graph.Selected.Set("node:s:aaaaaaaa")
	g := vm.Contexts.Slice()
	if len(g) != 3 || g[0].Name != "session aaaaaaaa" || g[0].Band != model.BandHot || g[1].Name != "general-purpose ag1" || g[1].Ratio != 0.2 || g[2].Name != "agent ag2" || g[2].Text != "- / 300k" {
		t.Fatalf("%+v", g)
	}
	// A sub-agent alone; one that is not in the snapshot; anything else: none.
	vm.Graph.Selected.Set("node:a:aaaaaaaa:ag1")
	if g := vm.Contexts.Slice(); len(g) != 1 || g[0].Ratio != 0.2 {
		t.Fatalf("%+v", g)
	}
	vm.Graph.Selected.Set("node:a:aaaaaaaa:nope")
	if vm.Contexts.Len() != 0 {
		t.Fatal("a vanished sub-agent has no gauge")
	}
	vm.Graph.Selected.Set("node:s:zzzzzzzz")
	if vm.Contexts.Len() != 0 {
		t.Fatal("a vanished session has no gauge")
	}
	vm.Graph.Selected.Set("node:r:o/a")
	if vm.Contexts.Len() != 0 {
		t.Fatal("a repository has no context")
	}
	// No window sent: the default one.
	sn := ctxSnap()
	sn.ContextWindow = 0
	vm.Apply(sn)
	if vm.Rows.At(0).CtxText != "225k / 1M" {
		t.Fatalf("%q", vm.Rows.At(0).CtxText)
	}
}

// The session's detail text says it too (for a reader who cannot see the bar).
func TestDetailTextStatesTheContext(t *testing.T) {
	lines := detailLines(ctxSnap(), "node:s:aaaaaaaa")
	found := false
	for _, l := range lines {
		found = found || l == "context: 225k / 300k"
	}
	if !found {
		t.Fatalf("%v", lines)
	}
	agent := detailLines(ctxSnap(), "node:a:aaaaaaaa:ag1")
	found = false
	for _, l := range agent {
		found = found || l == "context: 60k / 300k"
	}
	if !found {
		t.Fatalf("%v", agent)
	}
}

func TestAgeOfARowWithNoActivityIsOlderThanAnyFilter(t *testing.T) {
	vm := NewViewModel()
	sn := snap()
	sn.Sessions[2].LastActivity = time.Time{}
	vm.Apply(sn)
	if vm.Rows.At(2).Age != neverActive {
		t.Fatalf("%v", vm.Rows.At(2).Age)
	}
}
