package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/toolkit"
)

type colorSent struct{ key, color string }

func colorVM(t *testing.T) (*ViewModel, *[]colorSent) {
	t.Helper()
	vm := NewViewModel()
	u := usageFixture()
	u.Accounts[0].Color = "#12ab34"
	u.Capacity.Items[0].Color = "#12ab34"
	vm.Apply(withUsage(snap()))
	vm.Usage.Set(u)
	var got []colorSent
	vm.Color.Send = func(key, color string) { got = append(got, colorSent{key, color}) }
	return vm, &got
}

func TestColorBeginNeedsAKnownAccountSelected(t *testing.T) {
	vm, _ := colorVM(t)
	c := vm.Color
	if c.Begin.CanExecute() {
		t.Fatal("no row selected")
	}
	c.begin()
	if c.Open.Get() {
		t.Fatal("opened without a selection")
	}
	vm.CapSel.Set(2) // the unknown account
	if c.Begin.CanExecute() {
		t.Fatal("the unknown account was offered a colour")
	}
	vm.CapSel.Set(0)
	if !c.Begin.CanExecute() {
		t.Fatal("a known account")
	}
}

func TestColorStartsFromTheOwnersColourThenTheAutomaticOne(t *testing.T) {
	vm, got := colorVM(t)
	c := vm.Color
	vm.CapSel.Set(0)
	c.Begin.Execute()
	if p := c.Pick.Get(); !c.Open.Get() || p != (toolkit.RGBA{R: 0x12, G: 0xab, B: 0x34, A: 0xFF}) || len(*got) != 0 {
		t.Fatalf("%v", p)
	}
	c.Cancel.Execute()
	if c.Open.Get() || len(*got) != 0 {
		t.Fatal("cancel sends nothing")
	}
	vm.CapSel.Set(1) // no colour of its own: the second automatic one
	c.Begin.Execute()
	if p, want := c.Pick.Get(), lineColors(vm.Dark.Get())[1]; p != want {
		t.Fatalf("%v, want %v", p, want)
	}
	c.Cancel.Execute()
	// An account the table lists but the usage does not know: the first automatic one.
	if p, want := c.current("zzzz9999"), lineColors(vm.Dark.Get())[0]; p != want {
		t.Fatalf("%v, want %v", p, want)
	}
}

func TestColorConfirmSendsAnOpaqueHexResetSendsNothingAndDoneClosesOrReports(t *testing.T) {
	vm, got := colorVM(t)
	c := vm.Color
	vm.CapSel.Set(1)
	c.confirm() // not open: nothing
	if c.Confirm.CanExecute() || c.Reset.CanExecute() || c.Cancel.CanExecute() {
		t.Fatal("a closed dialog can be confirmed, reset or cancelled")
	}
	c.Begin.Execute()
	if !c.Confirm.CanExecute() || !c.Reset.CanExecute() || !c.Cancel.CanExecute() {
		t.Fatal("an open dialog cannot")
	}
	c.Pick.Set(toolkit.RGBA{R: 0xAB, G: 0xCD, B: 0xEF, A: 0x40})
	c.Confirm.Execute()
	if len(*got) != 1 || (*got)[0] != (colorSent{"bbbb2222", "#abcdef"}) || !c.Busy.Get() {
		t.Fatalf("%v busy=%v", *got, c.Busy.Get())
	}
	if c.Confirm.CanExecute() || c.Reset.CanExecute() {
		t.Fatal("confirm or reset while the server has the request")
	}
	c.Confirm.Execute() // busy: no second request
	c.Reset.Execute()
	if len(*got) != 1 {
		t.Fatal("a second request while busy")
	}
	c.Done(errors.New(strings.Repeat("x", 300)))
	if !c.Open.Get() || c.Busy.Get() || len(c.Err.Get()) > 125 || c.Err.Get() == "" {
		t.Fatalf("%v %v %q", c.Open.Get(), c.Busy.Get(), c.Err.Get())
	}
	c.Reset.Execute()
	if len(*got) != 2 || (*got)[1] != (colorSent{"bbbb2222", ""}) || c.Err.Get() != "" {
		t.Fatalf("%v", *got)
	}
	c.Done(nil)
	if c.Open.Get() || c.Busy.Get() {
		t.Fatal("success closes the dialog")
	}
	c.Send = nil
	c.Begin.Execute()
	c.Confirm.Execute()
	if c.Err.Get() == "" || c.Busy.Get() {
		t.Fatalf("%q", c.Err.Get())
	}
	c.Cancel.Execute()
}

func TestAccountInkTakesTheOwnersColourOnlyWhenItIsWellFormed(t *testing.T) {
	auto := lineColors(false)
	got := accountInk(model.AccountUsage{Color: "#ff0080"}, 1, auto)
	if got != (toolkit.RGBA{R: 0xff, G: 0x00, B: 0x80, A: 0xFF}) {
		t.Fatalf("%v", got)
	}
	for _, bad := range []string{"", "red", "#fff", "#gggggg", "url(x)"} {
		if got := accountInk(model.AccountUsage{Color: bad}, 1, auto); got != auto[1%len(auto)] {
			t.Fatalf("%q gave %v", bad, got)
		}
	}
}

// The colour the owner chose is the one the quota view is drawn in: the lines and
// the reset markers of that account.
func TestTheChosenColourShowsInTheRenderedQuotaView(t *testing.T) {
	u := usageFixture()
	want := toolkit.RGBA{R: 0x12, G: 0xab, B: 0x34, A: 0xFF}
	u.Accounts[0].Color = "#12ab34"
	v := buildQuota(u, 0, SpanWeek, KindOutput, false)
	seen := 0
	for _, l := range v.Official.Overlays {
		if l.Ink == want {
			seen++
		}
	}
	if seen == 0 {
		t.Fatalf("no overlay line is drawn in %v", want)
	}
	// And through a scene: a change of colour rebuilds the view.
	s := newScene(t)
	vm := s.VM()
	vm.Apply(withUsageOf(snap(), usageFixture()))
	vm.Tab.Set(TabQuota)
	pixels(s)
	vm.Usage.Set(u)
	pixels(s)
	found := false
	for _, l := range s.official.Overlays {
		if l.Ink == want {
			found = true
		}
	}
	if !found {
		t.Fatal("the scene's chart does not use the chosen colour")
	}
}

func TestColourDialogThroughTheWidgets(t *testing.T) {
	s := openQuota(t)
	vm := s.VM()
	var got []colorSent
	vm.Color.Send = func(k, c string) { got = append(got, colorSent{k, c}) }
	vm.CapSel.Set(1)
	bx, by := center(s.colorBtn.Bounds())
	s.HandleClick(bx, by)
	if !vm.Color.Open.Get() || len(s.overlay.Layers) != 1 {
		t.Fatal("the dialog did not open")
	}
	pixels(s)
	if g := s.targets(); len(g) != 1 || g[0] != toolkit.Widget(s.colorDlg) {
		t.Fatalf("%v", g)
	}
	dx, dy := center(s.colorDlg.Bounds())
	if !s.HandleScroll(dx, dy, 3) {
		t.Fatal("scroll")
	}
	if !s.HandleChar("t") || vm.PanelOpen.Get() {
		t.Fatal("a character reached what is behind the dialog")
	}
	// A drag in the picker keeps its grab.
	px, py := center(s.colorPicker.Bounds())
	s.HandleClick(5, 5)
	s.HandleClick(px, py)
	s.HandleMove(px+3, py+3)
	s.HandleRelease(px+3, py+3)
	pixels(s)
	// Enter confirms, Escape cancels, an unrelated key is ignored.
	s.HandleKeyDown("ArrowLeft")
	s.HandleKeyDown("Enter")
	if len(got) != 1 || got[0].key != "bbbb2222" || !strings.HasPrefix(got[0].color, "#") {
		t.Fatalf("%v", got)
	}
	vm.Color.Done(errors.New("nope"))
	if s.colorErr.Text().Get() != "nope" {
		t.Fatalf("%q", s.colorErr.Text().Get())
	}
	x, y := center(s.colorReset.Bounds())
	s.HandleClick(x, y)
	if len(got) != 2 || got[1].color != "" {
		t.Fatalf("%v", got)
	}
	vm.Color.Done(nil)
	if vm.Color.Open.Get() || len(s.overlay.Layers) != 0 {
		t.Fatal("the dialog did not go away")
	}
	s.HandleClick(bx, by)
	ox, oy := center(s.colorOK.Bounds())
	s.HandleClick(ox, oy)
	if len(got) != 3 {
		t.Fatalf("the OK button did not send: %v", got)
	}
	vm.Color.Done(nil)
	s.HandleClick(bx, by)
	s.HandleKeyDown("Escape")
	if vm.Color.Open.Get() {
		t.Fatal("Escape")
	}
	s.HandleClick(bx, by)
	nx, ny := center(s.colorNo.Bounds())
	s.HandleClick(nx, ny)
	if vm.Color.Open.Get() {
		t.Fatal("the Cancel button did not close")
	}
	if d, _, _ := s.openDialog(); d != nil {
		t.Fatal("no dialog is open")
	}
}

func TestAClickOnTheRenameEntryFocusesItAndDropsTheGrab(t *testing.T) {
	s := openQuota(t)
	vm := s.VM()
	vm.CapSel.Set(1)
	vm.Rename.Begin.Execute()
	pixels(s)
	s.renameEntry.SetFocused(false)
	ex, ey := center(s.renameEntry.Bounds())
	s.HandleClick(ex, ey)
	if !s.renameEntry.Focused() {
		t.Fatal("a click on the entry did not focus it")
	}
	vm.Rename.Cancel.Execute()
}

func TestTheRenameDialogTakesTheClickNotTheDrag(t *testing.T) {
	s := openQuota(t)
	vm := s.VM()
	vm.CapSel.Set(1)
	vm.Rename.Begin.Execute()
	pixels(s)
	ex, ey := center(s.renameEntry.Bounds())
	s.HandleClick(ex, ey)
	if s.HandleRelease(ex, ey) {
		t.Fatal("the Rename dialog kept a grab after a click")
	}
	vm.Rename.Cancel.Execute()
	vm.CapSel.Set(1)
	vm.Color.Begin.Execute()
	pixels(s)
	px, py := center(s.colorPicker.Bounds())
	s.HandleClick(px, py)
	if !s.HandleRelease(px, py) {
		t.Fatal("the Colour dialog dropped the picker grab")
	}
	vm.Color.Cancel.Execute()
}
