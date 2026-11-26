package ui

import "testing"

// With no way to send, the Colour dialog says so. The refusal is what keeps the
// nil Send from being called; if it is deleted the test fails here, on an
// assertion, and not by a crash further on.
func TestColourWithNoSendSaysSoWithoutCallingIt(t *testing.T) {
	vm, _ := colorVM(t)
	vm.Color.Send = nil
	vm.CapSel.Set(1)
	vm.Color.Begin.Execute()
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("a nil Send was called: %v", r)
			}
		}()
		vm.Color.Confirm.Execute()
	}()
	if vm.Color.Err.Get() == "" || vm.Color.Busy.Get() {
		t.Errorf("err %q busy %v", vm.Color.Err.Get(), vm.Color.Busy.Get())
	}
}
