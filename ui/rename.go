package ui

import (
	"strings"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/mvvm"
)

// RenameState is the Rename action of the accounts table: a dialog that gives the
// selected account a display name. It holds all of the dialog's state; the Scene
// only shows it. Nothing is saved until Confirm: a suggestion (the organisation's
// name, when the server was started with --show-org) is only the text the entry
// starts with.
type RenameState struct {
	vm *ViewModel

	// Open is whether the dialog is up; Text the entry's content; Err why the last
	// attempt failed ("" when it did not); Busy while the server has the request.
	Open *mvvm.Observable[bool]
	Text *mvvm.Observable[string]
	Err  *mvvm.Observable[string]
	Busy *mvvm.Observable[bool]
	// Begin opens the dialog on the selected account; Confirm sends the name;
	// Cancel closes it. Begin can run only with an account selected.
	Begin, Confirm, Cancel *mvvm.Command

	// Send asks the server to rename account key to name; the host sets it and it
	// must not block (the browser's event loop must run for the request to go
	// out), reporting back through Done.
	Send func(key, name string)

	key string // the account being renamed
}

func newRenameState(vm *ViewModel) *RenameState {
	r := &RenameState{
		vm:   vm,
		Open: mvvm.NewObservable(false), Text: mvvm.NewObservable(""),
		Err: mvvm.NewObservable(""), Busy: mvvm.NewObservable(false),
	}
	r.Begin = mvvm.NewCommand(r.begin, r.canBegin)
	r.Confirm = mvvm.NewCommand(r.confirm, func() bool { return r.Open.Get() && !r.Busy.Get() })
	r.Cancel = mvvm.NewCommand(r.cancel, func() bool { return r.Open.Get() })
	vm.CapSel.SubscribeChanged(r.Begin.RaiseCanExecuteChanged)
	vm.Capacity.SubscribeChanged(r.Begin.RaiseCanExecuteChanged)
	return r
}

// selected is the account of the selected accounts-table row, or "" when there is
// none or it is the unknown account (which cannot be named: it may be any).
func (r *RenameState) selected() (key string, c model.CapacityItem, ok bool) {
	u, i := r.vm.Usage.Get(), r.vm.CapSel.Get()
	if u == nil || i < 0 || i >= len(u.Capacity.Items) {
		return "", model.CapacityItem{}, false
	}
	c = u.Capacity.Items[i]
	return c.Key, c, c.Key != ""
}

func (r *RenameState) canBegin() bool {
	_, _, ok := r.selected()
	return ok
}

func (r *RenameState) begin() {
	key, c, ok := r.selected()
	if !ok {
		return
	}
	r.key = key
	start := c.Alias
	if start == "" { // the suggestion is only a start; the owner confirms it
		if u := r.vm.Usage.Get(); u != nil {
			for _, a := range u.Accounts {
				if a.Key == key {
					start = a.Suggest
				}
			}
		}
	}
	r.Err.Set("")
	r.Busy.Set(false)
	r.Text.Set(start)
	r.Open.Set(true)
	r.Confirm.RaiseCanExecuteChanged()
	r.Cancel.RaiseCanExecuteChanged()
}

func (r *RenameState) confirm() {
	if !r.Open.Get() || r.Busy.Get() {
		return
	}
	name := strings.TrimSpace(r.Text.Get())
	if r.Send == nil {
		r.Err.Set("this view cannot rename accounts")
		return
	}
	r.Err.Set("")
	r.Busy.Set(true)
	r.Confirm.RaiseCanExecuteChanged()
	r.Send(r.key, name)
}

func (r *RenameState) cancel() {
	r.Open.Set(false)
	r.Busy.Set(false)
	r.Confirm.RaiseCanExecuteChanged()
	r.Cancel.RaiseCanExecuteChanged()
}

// Done is the server's answer to Send: the dialog closes on success and says why
// on failure, keeping what the owner typed.
func (r *RenameState) Done(err error) {
	r.Busy.Set(false)
	if err != nil {
		r.Err.Set(model.Truncate(err.Error(), 120))
		r.Confirm.RaiseCanExecuteChanged()
		return
	}
	r.cancel()
}
