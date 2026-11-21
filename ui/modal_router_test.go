package ui

import "testing"

// The Rename dialog is modal: a press on the panel's divider, which is not the
// dialog's, is swallowed and starts no drag of the splitter beneath.
func TestAModalDialogSwallowsAPressOnTheDivider(t *testing.T) {
	s := newScene(t)
	x, y := open(t, s)
	s.VM().Rename.Open.Set(true)
	pixels(s)
	if len(s.edges()) != 0 {
		t.Fatalf("%d dividers grab a press while the dialog is open", len(s.edges()))
	}
	s.HandleClick(x, y)
	if s.split.Dragging().Get() {
		t.Fatal("the divider was grabbed through the modal dialog")
	}
	s.VM().Rename.Open.Set(false)
	pixels(s)
	if len(s.edges()) != 2 {
		t.Fatalf("%d dividers with the panel open and no dialog, want 2", len(s.edges()))
	}
}
