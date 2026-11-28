package ui

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

// A node label is measured, not counted: "acme-demo/api-gateway" is 21 characters
// and was cut by the chip's edge.
func TestNodeLabelsFitTheirChip(t *testing.T) {
	for _, s := range []string{"", "api", "acme-demo/api-gateway", "acme-demo/api-gateway-with-a-very-long-name", "WWWWWWWWWWWWWWWWWWWWWWWWWWWW"} {
		got := fitNodeLabel(s)
		if w := toolkit.TextWidth(got); w > nodeTextW {
			t.Errorf("%q -> %q is %d wide in %d", s, got, w, nodeTextW)
		}
		if toolkit.TextWidth(s) <= nodeTextW && got != s {
			t.Errorf("%q fits and was changed to %q", s, got)
		}
	}
	if got := fitNodeLabel("acme-demo/api-gateway-with-a-very-long-name"); got[len(got)-3:] != "…" {
		t.Errorf("a cut label ends with an ellipsis, got %q", got)
	}
	// Nothing fits: the ellipsis alone.
	if got := fitNodeLabelIn("WWWWWWWW", 1); got != "…" {
		t.Errorf("got %q", got)
	}
}

// The search field draws a magnifier, not the toolkit's pixel square.
func TestSearchIconIsAMagnifier(t *testing.T) {
	s := NewScene(1280, 720)
	if s.search.Icon == nil {
		t.Fatal("the search has no icon")
	}
}

// The header's fixed-width controls must hold their text in every language and
// every choice: a "Theme: Light" cut by its own chevron, or "Sessions (t)" wrapped
// onto two lines, was visible in the live screen and in every capture.
func TestHeaderControlsHoldTheirText(t *testing.T) {
	const (
		dropdownLeft = 6  // the text's left inset in a DropDown
		chevron      = 16 // the room the chevron takes on the right edge
		buttonPad    = 28 // the horizontal padding a Button leaves around its label
	)
	s := NewScene(1280, 720)
	for lang := 0; lang < 3; lang++ {
		s.vm.Prefs.LangSel.Set(lang)
		for th := 0; th < 3; th++ {
			s.vm.Prefs.ThemeSel.Set(th)
			s.Draw(make([]byte, 1280*720*4)) // lays the screen out
			dd := s.themeDD
			if w := toolkit.TextWidth(dd.Current()); w+dropdownLeft+chevron > dd.Bounds().W {
				t.Errorf("lang %d: %q is %d wide in a %d wide drop-down", lang, dd.Current(), w, dd.Bounds().W)
			}
			dd = s.langDD
			if w := toolkit.TextWidth(dd.Current()); w+dropdownLeft+chevron > dd.Bounds().W {
				t.Errorf("lang %d: %q is %d wide in a %d wide drop-down", lang, dd.Current(), w, dd.Bounds().W)
			}
		}
		label := s.toggle.Label().Get()
		if w := toolkit.TextWidth(label); w+buttonPad > s.toggle.Bounds().W {
			t.Errorf("lang %d: %q is %d wide in a %d wide button", lang, label, w, s.toggle.Bounds().W)
		}
	}
	// The header leaves the search a usable field.
	if w := s.search.Bounds().W; w < 120 {
		t.Errorf("the search is %d wide", w)
	}
}
