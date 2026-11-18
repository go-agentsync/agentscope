package ui

import (
	"github.com/go-widgets/mvvm"
	"github.com/go-widgets/toolkit"
)

// bindTableRows is the one-way sink mvvmtk has no binder for yet (a Table's
// Rows from an ObservableList): on every list change it rebuilds the table's
// row slice by project and calls invalidate. It lives in a *_binding.go file,
// the place mvvmlint reserves for exactly this glue, until mvvmtk grows
// BindTableRows (go-widgets/mvvmtk#44).
func bindTableRows[T any](t *toolkit.Table, l *mvvm.ObservableList[T], project func(T) []string, invalidate func()) (unbind func()) {
	sync := func() {
		items := l.Slice()
		rows := make([][]string, len(items))
		for i, it := range items {
			rows[i] = project(it)
		}
		t.Rows = rows
		if invalidate != nil {
			invalidate()
		}
	}
	sync()
	return l.SubscribeChanged(sync)
}
