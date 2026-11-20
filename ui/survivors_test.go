package ui

import (
	"reflect"
	"testing"

	"github.com/go-agentsync/agentscope/model"
)

// Each test below holds a refusal or an ordering that a mutsweep of v0.9.0's
// code found nothing held.

func TestSyncingTheFiltersIsNotAChangeTheOwnerMade(t *testing.T) {
	changed := 0
	f := newFilters(func() { changed++ })
	rows := func(models ...string) []Row {
		var out []Row
		for _, m := range models {
			out = append(out, Row{Model: m, Repos: "o/a\no/b"})
		}
		return out
	}
	f.sync(rows("m1", "m1", "m2"))
	// The owner picks the second repository and the most used model: changes.
	f.Repo.Set(2)
	f.ModelOn[0].Set(true)
	if changed != 2 || f.repoSel != "o/b" || !f.models["m1"] {
		t.Fatalf("changed %d, repo %q, models %v", changed, f.repoSel, f.models)
	}
	changed = 0
	// A new listing moves both choices to other slots, by name: that is the
	// view following the data, and no filter has changed.
	f.sync(append(rows("m2", "m2", "m2"), Row{Model: "m1", Repos: "o/a\no/0"}))
	if changed != 0 {
		t.Fatalf("syncing the offered choices ran the filter %d times", changed)
	}
	if f.repoSel != "o/b" || !f.models["m1"] || len(f.models) != 1 {
		t.Fatalf("a choice was lost in the sync: repo %q, models %v", f.repoSel, f.models)
	}
	if f.ModelLabels[0].Get() != "m2" || f.ModelOn[0].Get() || f.ModelLabels[1].Get() != "m1" || !f.ModelOn[1].Get() {
		t.Fatalf("labels %q %q, on %v %v", f.ModelLabels[0].Get(), f.ModelLabels[1].Get(), f.ModelOn[0].Get(), f.ModelOn[1].Get())
	}
}

func TestModelCheckBoxesAreOrderedByUseThenByName(t *testing.T) {
	f := newFilters(func() {})
	var rows []Row
	for _, m := range []string{"a", "z", "z", "z", "m", "m"} {
		rows = append(rows, Row{Model: m})
	}
	f.sync(rows)
	var got []string
	for i := 0; i < 3; i++ {
		got = append(got, f.ModelLabels[i].Get())
	}
	if want := []string{"z", "m", "a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("%v, want the most used first: %v", got, want)
	}
	// Equal use: by name.
	f.sync([]Row{{Model: "b"}, {Model: "a"}})
	if f.ModelLabels[0].Get() != "a" || f.ModelLabels[1].Get() != "b" {
		t.Fatalf("%q %q", f.ModelLabels[0].Get(), f.ModelLabels[1].Get())
	}
}

func TestParseColWWantsOneWidthPerColumn(t *testing.T) {
	var ok []string
	for range ColMin {
		ok = append(ok, "90")
	}
	good := formatColW(func() []int {
		var ws []int
		for range ok {
			ws = append(ws, 90)
		}
		return ws
	}())
	if ws, fine := parseColW(good); !fine || len(ws) != len(ColMin) {
		t.Fatalf("%q: %v %v", good, ws, fine)
	}
	for _, bad := range []string{"", "90", good + ",90", "90,90"} {
		if ws, fine := parseColW(bad); fine {
			t.Errorf("%q was read as %v", bad, ws)
		}
	}
}

func TestAnAccountWithNoKeyIsNamedAsRecordedBeforeAccountsWereToldApart(t *testing.T) {
	got := accountName(model.AccountUsage{Label: "Claude Code", Plan: "max"})
	if got != "unknown account (recorded before accounts were told apart)" {
		t.Fatalf("%q", got)
	}
	if got := accountName(model.AccountUsage{Key: "abcdef12", Label: "Claude Code", Plan: "max"}); got == "" || got == "unknown account" {
		t.Fatalf("a keyed account: %q", got)
	}
}

// The panel stays "open" while the Quota tab is shown, but it is not on screen:
// its divider must neither light up nor show a resize cursor there.
func TestThePanelsDividerIsInertOnTheQuotaTab(t *testing.T) {
	s := newScene(t)
	x, y := open(t, s)
	s.HandleMove(x, y)
	if s.hover == -1 || s.Cursor(x, y) != "col-resize" {
		t.Fatalf("on the Graph tab: hover %d, cursor %q", s.hover, s.Cursor(x, y))
	}
	s.VM().Tab.Set(TabQuota)
	if got := s.Cursor(x, y); got != "" {
		t.Fatalf("the Quota tab shows %q over where the divider was", got)
	}
	if !s.HandleMove(x, y) || s.hover != -1 {
		t.Fatalf("the hover must be dropped when the panel leaves the screen: hover %d", s.hover)
	}
}

func TestAClosedPanelHasNoDividerToHoverOrDrag(t *testing.T) {
	s := newScene(t)
	x, y := open(t, s)
	s.HandleMove(x, y) // hovering the divider of an open panel
	if got := s.Cursor(x, y); got != "col-resize" {
		t.Fatalf("open: %q", got)
	}
	s.VM().PanelOpen.Set(false)
	if got := s.Cursor(x, y); got != "" {
		t.Fatalf("closed: the old divider still shows %q", got)
	}
	if !s.HandleMove(x, y) {
		t.Fatal("closing the panel must drop the hover, which is a change of picture")
	}
	if s.hover != -1 {
		t.Fatalf("hover %d", s.hover)
	}
	if s.inPanel(nil) {
		t.Fatal("no widget is in the panel")
	}
}
