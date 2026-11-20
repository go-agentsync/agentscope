package ui

import (
	"testing"

	"github.com/go-agentsync/agentscope/model"
)

func TestAccountSelectorSwitchesBetweenAggregateAndOneAccount(t *testing.T) {
	s := newScene(t)
	vm := s.VM()
	vm.Apply(withUsage(snap()))
	vm.Tab.Set(TabQuota)
	if vm.Account.Get() != 0 || vm.AccountOptions.Len() != 4 || vm.Capacity.Len() != 3 || s.quota.Visible().Get() != "allrange" {
		t.Fatalf("starts on All: account %d, %d options, %d capacity rows, page %q", vm.Account.Get(), vm.AccountOptions.Len(), vm.Capacity.Len(), s.quota.Visible().Get())
	}
	if len(s.outBars.Layers) != 5 || len(s.capTable.Rows) != 3 || s.accountDD.Options[1] != "Claude Code (max) #aaaa" {
		t.Fatal("the aggregate has five layers, the capacity table its rows, the selector its options")
	}
	// Choosing an account, through the widget, drives the view model and the page.
	s.accountDD.Select(2)
	if vm.Account.Get() != 2 || s.quota.Visible().Get() != "range" || len(s.outBars.Layers) != 4 {
		t.Fatalf("account %d page %q layers %d", vm.Account.Get(), s.quota.Visible().Get(), len(s.outBars.Layers))
	}
	vm.Span.Set(SpanWeeks)
	if s.quota.Visible().Get() != "weeks" || len(s.weekBars.Layers[0]) != 4 {
		t.Fatalf("one account in the weeks span: %q", s.quota.Visible().Get())
	}
	pixels(s)

	// The selection follows its account, by key, when the usage is rebuilt.
	u2 := usageFixture()
	u2.Accounts[0], u2.Accounts[1] = u2.Accounts[1], u2.Accounts[0] // the order changed
	vm.Apply(withUsageOf(snap(), u2))
	if vm.Account.Get() != 1 || vm.selKey != "bbbb2222" {
		t.Fatalf("B moved to position 1 and stays selected: %d %q", vm.Account.Get(), vm.selKey)
	}
	// The selected account vanishes: back to the aggregate, not to another account.
	u3 := usageFixture()
	u3.Accounts = u3.Accounts[:1]
	vm.Apply(withUsageOf(snap(), u3))
	if vm.Account.Get() != 0 || vm.selKey != allKey {
		t.Fatalf("%d %q", vm.Account.Get(), vm.selKey)
	}
	// No usage at all: only the aggregate is on offer.
	vm.Apply(snap())
	if vm.AccountOptions.Len() != 1 || vm.Capacity.Len() != 0 || vm.Account.Get() != 0 {
		t.Fatal("no usage, no accounts")
	}
	vm.Account.Set(3) // an index beyond the accounts is the aggregate
	if vm.selKey != allKey {
		t.Fatal("selection beyond the accounts")
	}
}

func TestAccountDropDownOpensAndAClickElsewhereClosesIt(t *testing.T) {
	s := newScene(t)
	vm := s.VM()
	vm.Apply(withUsage(snap()))
	vm.Tab.Set(TabQuota)
	pixels(s)
	x, y := center(s.accountDD.Bounds())
	s.HandleClick(x, y)
	if !s.accountDD.PopoverOpen() {
		t.Fatal("the drop-down did not open")
	}
	pixels(s) // the host draws the popover
	// A click on an option picks it.
	pb := s.accountDD.PopoverBounds()
	s.HandleClick(pb.X+pb.W/2, pb.Y+pb.H*3/8)
	if s.accountDD.PopoverOpen() {
		t.Fatal("picking an option closes the list")
	}
	s.HandleClick(x, y)
	s.HandleClick(5, testH-5) // outside the list
	if s.accountDD.PopoverOpen() {
		t.Fatal("a click outside closes the list")
	}
}

func withUsageOf(sn model.Snapshot, u *model.Usage) model.Snapshot {
	sn.Usage = u
	return sn
}

func TestMarkerLabelNamesAccountsExceptTheUnknownOne(t *testing.T) {
	if markerLabel(model.AccountUsage{}, " reset") != "" || markerLabel(model.AccountUsage{Key: "abcd1234"}, " reset") != "account #abcd reset" {
		t.Fatal("marker labels")
	}
}

func TestCapacityRowOfAKnownAccountWhoseResetPassedIsStaleAndNotCounted(t *testing.T) {
	rows := capacityRows(model.Capacity{Items: []model.CapacityItem{{Key: "abcd1234", Label: "Claude Code", Stale: true}}})
	if rows[0].Weekly != "stale" || rows[0].Left != "-" {
		t.Fatalf("%+v", rows[0])
	}
}
