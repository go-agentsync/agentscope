package ui

import (
	"github.com/go-widgets/mvvm"
	"github.com/go-widgets/toolkit"
)

// bindWrapList keeps a WrapList's items equal to a list of lines: seeded now,
// rewritten on every change, and invalidate (may be nil) fires after each. The
// widget breaks the lines to its own width; the binding hands it the raw text.
func bindWrapList(w *toolkit.WrapList, l *mvvm.ObservableList[string], invalidate func()) (unbind func()) {
	sync := func() {
		w.SetItems(l.Slice())
		if invalidate != nil {
			invalidate()
		}
	}
	sync()
	return l.SubscribeChanged(sync)
}
