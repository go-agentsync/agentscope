package ui

import (
	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/mvvm"
	"github.com/go-widgets/toolkit"
)

// ColorState is the Colour action of the accounts table: a dialog with the
// toolkit's colour picker that gives the selected account a colour of its own, as
// RenameState gives it a name. It holds all of the dialog's state; the Scene only
// shows it. Nothing is saved until Confirm, and Reset restores the automatic
// colour. The colour is sent, and stored, as "#rrggbb": the picker's alpha is
// never kept, a line is always opaque.
type ColorState struct {
	vm *ViewModel

	// Open is whether the dialog is up; Pick the picker's colour (bound two ways to
	// the toolkit's ColorPicker); Err why the last attempt failed ("" when it did
	// not); Busy while the server has the request.
	Open *mvvm.Observable[bool]
	Pick *mvvm.Observable[toolkit.RGBA]
	Err  *mvvm.Observable[string]
	Busy *mvvm.Observable[bool]
	// Begin opens the dialog on the selected account; Confirm sends the picked
	// colour; Reset asks for the automatic one back; Cancel closes it. Begin can
	// run only with an account selected.
	Begin, Confirm, Reset, Cancel *mvvm.Command

	// Send asks the server to give account key the colour ("#rrggbb", or "" for
	// the automatic one); the host sets it and it must not block, reporting back
	// through Done.
	Send func(key, color string)

	key string // the account being coloured
}

func newColorState(vm *ViewModel) *ColorState {
	c := &ColorState{
		vm:   vm,
		Open: mvvm.NewObservable(false), Pick: mvvm.NewObservable(toolkit.RGBA{A: 0xFF}),
		Err: mvvm.NewObservable(""), Busy: mvvm.NewObservable(false),
	}
	idle := func() bool { return c.Open.Get() && !c.Busy.Get() }
	c.Begin = mvvm.NewCommand(c.begin, func() bool { _, _, ok := vm.Rename.selected(); return ok })
	c.Confirm = mvvm.NewCommand(c.confirm, idle)
	c.Reset = mvvm.NewCommand(c.reset, idle)
	c.Cancel = mvvm.NewCommand(c.cancel, func() bool { return c.Open.Get() })
	vm.CapSel.SubscribeChanged(c.Begin.RaiseCanExecuteChanged)
	vm.Capacity.SubscribeChanged(c.Begin.RaiseCanExecuteChanged)
	return c
}

// current is the colour the account is drawn in now: the owner's, else the
// automatic one by its position, in the theme in use.
func (c *ColorState) current(key string) toolkit.RGBA {
	u := c.vm.Usage.Get()
	auto := lineColors(c.vm.Dark.Get())
	for i, a := range u.Accounts {
		if a.Key == key {
			return accountInk(a, i, auto)
		}
	}
	return auto[0]
}

func (c *ColorState) begin() {
	key, _, ok := c.vm.Rename.selected()
	if !ok {
		return
	}
	c.key = key
	c.Err.Set("")
	c.Busy.Set(false)
	c.Pick.Set(c.current(key))
	c.Open.Set(true)
	c.raise()
}

// raise tells the commands what the dialog's state now allows.
func (c *ColorState) raise() {
	c.Confirm.RaiseCanExecuteChanged()
	c.Reset.RaiseCanExecuteChanged()
	c.Cancel.RaiseCanExecuteChanged()
}

func (c *ColorState) confirm() {
	p := c.Pick.Get()
	c.send(model.FormatColor(p.R, p.G, p.B))
}

func (c *ColorState) reset() { c.send("") }

func (c *ColorState) send(color string) {
	if !c.Open.Get() || c.Busy.Get() {
		return
	}
	if c.Send == nil {
		c.Err.Set(T("colour.unsupported"))
		return
	}
	c.Err.Set("")
	c.Busy.Set(true)
	c.raise()
	c.Send(c.key, color)
}

func (c *ColorState) cancel() {
	c.Open.Set(false)
	c.Busy.Set(false)
	c.raise()
}

// Done is the server's answer to Send: the dialog closes on success and says why
// on failure, keeping the colour the owner picked.
func (c *ColorState) Done(err error) {
	c.Busy.Set(false)
	if err != nil {
		c.Err.Set(model.Truncate(err.Error(), 120))
		c.raise()
		return
	}
	c.cancel()
}
