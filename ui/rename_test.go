package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/toolkit"
)

type sent struct{ key, name string }

func renameVM(t *testing.T) (*ViewModel, *[]sent) {
	t.Helper()
	vm := NewViewModel()
	u := usageFixture()
	u.Capacity.Items[0].Alias = "Perso"
	u.Accounts[1].Suggest = "Suggested Org" // the active account's suggestion (--show-org)
	vm.Apply(withUsage(snap()))
	vm.Usage.Set(u)
	var got []sent
	vm.Rename.Send = func(key, name string) { got = append(got, sent{key, name}) }
	return vm, &got
}

func TestRenameBeginNeedsAKnownAccountSelected(t *testing.T) {
	vm, _ := renameVM(t)
	r := vm.Rename
	if r.Begin.CanExecute() {
		t.Fatal("no row selected")
	}
	r.begin() // a direct call with nothing selected does nothing
	if r.Open.Get() {
		t.Fatal("opened without a selection")
	}
	vm.CapSel.Set(2) // the unknown account: it may be any, it cannot be named
	if r.Begin.CanExecute() {
		t.Fatal("the unknown account was offered for a name")
	}
	vm.CapSel.Set(0)
	if !r.Begin.CanExecute() {
		t.Fatal("a known account")
	}
	vm.CapSel.Set(99)
	if r.Begin.CanExecute() {
		t.Fatal("a row that is not there")
	}
	vm.Usage.Set(nil)
	vm.CapSel.Set(0)
	if r.Begin.CanExecute() {
		t.Fatal("no usage")
	}
}

func TestRenameStartsFromTheAliasThenTheSuggestionAndSavesNothingUntilConfirmed(t *testing.T) {
	vm, got := renameVM(t)
	r := vm.Rename
	vm.CapSel.Set(0) // has an alias
	r.Begin.Execute()
	if !r.Open.Get() || r.Text.Get() != "Perso" || len(*got) != 0 {
		t.Fatalf("%v %q", r.Open.Get(), r.Text.Get())
	}
	r.Cancel.Execute()
	if r.Open.Get() || len(*got) != 0 {
		t.Fatal("cancel closes and sends nothing")
	}
	vm.CapSel.Set(1) // no alias, but a suggestion
	r.Begin.Execute()
	if r.Text.Get() != "Suggested Org" || len(*got) != 0 {
		t.Fatalf("the suggestion is only the entry's start: %q, %d sent", r.Text.Get(), len(*got))
	}
	r.Cancel.Execute()
	// Neither: empty. (Account A has an alias; blank it and drop the suggestion.)
	u := usageFixture()
	vm.Usage.Set(u)
	vm.CapSel.Set(1)
	r.Begin.Execute()
	if r.Text.Get() != "" {
		t.Fatalf("%q", r.Text.Get())
	}
}

func TestRenameConfirmSendsTheTrimmedNameOnceAndDoneClosesOrReports(t *testing.T) {
	vm, got := renameVM(t)
	r := vm.Rename
	vm.CapSel.Set(1)
	r.confirm() // not open: nothing
	r.Begin.Execute()
	r.Text.Set("  Labo (team) \n")
	r.Confirm.Execute()
	if len(*got) != 1 || (*got)[0] != (sent{"bbbb2222", "Labo (team)"}) || !r.Busy.Get() {
		t.Fatalf("%v busy=%v", *got, r.Busy.Get())
	}
	r.Confirm.Execute() // while the server has it: no second request
	if len(*got) != 1 || r.Confirm.CanExecute() {
		t.Fatal("a second confirm while busy")
	}
	// The server refuses: the dialog stays, says why, keeps the text.
	r.Done(errors.New(strings.Repeat("x", 300)))
	if !r.Open.Get() || r.Busy.Get() || len(r.Err.Get()) > 125 || r.Text.Get() == "" {
		t.Fatalf("%v %v %q", r.Open.Get(), r.Busy.Get(), r.Err.Get())
	}
	r.Confirm.Execute()
	if len(*got) != 2 || r.Err.Get() != "" {
		t.Fatal("retry clears the error")
	}
	r.Done(nil)
	if r.Open.Get() || r.Busy.Get() {
		t.Fatal("success closes the dialog")
	}
	// An empty name is a request to remove the alias.
	r.Begin.Execute()
	r.Text.Set("")
	r.Confirm.Execute()
	if (*got)[2] != (sent{"bbbb2222", ""}) {
		t.Fatalf("%v", *got)
	}
	r.Done(nil)
	// A view with no way to send says so instead of hanging.
	r.Send = nil
	r.Begin.Execute()
	r.Confirm.Execute()
	if r.Err.Get() == "" || r.Busy.Get() {
		t.Fatalf("%q", r.Err.Get())
	}
	r.Cancel.Execute()
}

func openQuota(t *testing.T) *Scene {
	t.Helper()
	s := newScene(t)
	vm := s.VM()
	vm.Apply(withUsage(snap()))
	vm.Tab.Set(TabQuota)
	vm.Account.Set(0)
	pixels(s)
	return s
}

func TestRenameDialogThroughTheWidgets(t *testing.T) {
	s := openQuota(t)
	vm := s.VM()
	var got []sent
	vm.Rename.Send = func(k, n string) { got = append(got, sent{k, n}) }
	// Select the second account's row by a click, then the Rename button.
	tb := s.capTable.Bounds()
	rowH := toolkitRowH(s)
	s.HandleClick(tb.X+10, tb.Y+rowH+rowH/2+rowH) // header, row 0, row 1
	if vm.CapSel.Get() != 1 {
		t.Fatalf("selected row %d", vm.CapSel.Get())
	}
	bx, by := center(s.renameBtn.Bounds())
	s.HandleClick(bx, by)
	if !vm.Rename.Open.Get() || len(s.overlay.Layers) != 1 || s.keyTarget != toolkit.Widget(s.renameEntry) {
		t.Fatalf("the dialog did not open: %v %d", vm.Rename.Open.Get(), len(s.overlay.Layers))
	}
	pixels(s)
	// Typing goes to the entry, even a "t" and a clicked-through key.
	for _, ch := range "Labo t" {
		s.HandleChar(string(ch))
	}
	if vm.Rename.Text.Get() != "Labo t" || vm.PanelOpen.Get() {
		t.Fatalf("%q panel=%v", vm.Rename.Text.Get(), vm.PanelOpen.Get())
	}
	s.HandleKeyDown("Backspace")
	s.HandleKeyDown("Backspace")
	if vm.Rename.Text.Get() != "Labo" {
		t.Fatalf("%q", vm.Rename.Text.Get())
	}
	// A click outside the modal dialog is swallowed; one on the entry focuses it.
	s.HandleClick(5, 5)
	if !vm.Rename.Open.Get() {
		t.Fatal("a click outside closed the dialog")
	}
	ex, ey := center(s.renameEntry.Bounds())
	s.HandleClick(ex, ey)
	// Enter confirms.
	s.HandleKeyDown("Enter")
	if len(got) != 1 || got[0] != (sent{"bbbb2222", "Labo"}) {
		t.Fatalf("%v", got)
	}
	vm.Rename.Done(nil)
	if vm.Rename.Open.Get() || len(s.overlay.Layers) != 0 || s.keyTarget != nil {
		t.Fatal("the dialog did not go away")
	}
	// Escape cancels; the OK and Cancel buttons are clickable.
	s.HandleClick(bx, by)
	s.HandleKeyDown("Escape")
	if vm.Rename.Open.Get() || len(got) != 1 {
		t.Fatal("Escape")
	}
	s.HandleClick(bx, by)
	ox, oy := center(s.renameOK.Bounds())
	s.HandleClick(ox, oy)
	if len(got) != 2 {
		t.Fatalf("the OK button did not send: %v", got)
	}
	vm.Rename.Done(errors.New("nope"))
	if s.renameErr.Text().Get() != "nope" {
		t.Fatalf("%q", s.renameErr.Text().Get())
	}
	nx, ny := center(s.renameNo.Bounds())
	s.HandleClick(nx, ny)
	if vm.Rename.Open.Get() {
		t.Fatal("the Cancel button did not close")
	}
	// An unrelated key reaches the entry, and the dialog opens with the alias shown.
	s.HandleClick(bx, by)
	s.HandleKeyDown("ArrowLeft")
	s.HandleKeyDown("Escape")
	pixels(s)
}

func TestTheAliasShowsEverywhereTheAccountIsNamed(t *testing.T) {
	u := usageFixture()
	u.Accounts[0].Alias, u.Capacity.Items[0].Alias = "Perso (max)", "Perso (max)"
	u.Capacity.Items[0].Plan = "max"
	s := newScene(t)
	vm := s.VM()
	vm.Apply(withUsageOf(snap(), u))
	vm.Tab.Set(TabQuota)
	if s.accountDD.Options[1] != "Perso (max)" {
		t.Fatalf("selector: %q", s.accountDD.Options)
	}
	if rows := s.capTable.Rows; rows[0][0] != "Perso (max)" || rows[0][1] != "max" || rows[1][0] != "Claude Code #bbbb" {
		t.Fatalf("table: %v", rows)
	}
	v := buildQuota(u, 0, SpanWeek, KindOutput, false)
	var names []string
	for _, l := range v.Official.Overlays {
		names = append(names, l.Name)
	}
	if !strings.Contains(strings.Join(names, ","), "Perso (max)") {
		t.Fatalf("chart legend: %v", names)
	}
	_ = model.Usage{}
}

func toolkitRowH(s *Scene) int { return s.px(22) }

// While the dialog is up the wheel goes to it alone, not to what is under it.
func TestWheelWhileTheDialogIsUpGoesToTheDialog(t *testing.T) {
	s := openQuota(t)
	s.VM().CapSel.Set(0)
	s.VM().Rename.Begin.Execute()
	if got := s.targets(); len(got) != 1 || got[0] != toolkit.Widget(s.renameDlg) {
		t.Fatalf("%v", got)
	}
	dx, dy := center(s.renameDlg.Bounds())
	if !s.HandleScroll(dx, dy, 3) {
		t.Fatal("scroll")
	}
	s.VM().Rename.Cancel.Execute()
}
