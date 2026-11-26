package ui

import (
	"strconv"
	"strings"
)

// The side panel's width, in logical pixels: it opens at PanelDefaultW, the
// owner may drag its left edge, and it stays between PanelMinW and
// PanelMaxPercent of the surface.
const (
	PanelMinW       = 320
	PanelDefaultW   = 420
	PanelMaxPercent = 70
	maxColW         = 1600
)

// ClampPanelW is w held between PanelMinW and PanelMaxPercent of viewW (both
// logical pixels). A surface too narrow for the minimum gets the minimum: the
// panel never becomes unusable.
func ClampPanelW(w, viewW int) int {
	hi := max(viewW*PanelMaxPercent/100, PanelMinW)
	return min(max(w, PanelMinW), hi)
}

// EffectivePanelW is the width the panel is shown at: the chosen PanelW held to
// the bounds of the surface as it is now.
func (vm *ViewModel) EffectivePanelW() int { return ClampPanelW(vm.PanelW.Get(), vm.ViewW.Get()) }

// ColTitles are the sessions table's column titles, in the language in use.
func ColTitles() []string {
	return []string{T("col.state"), T("col.session"), T("col.context"), T("col.model"), T("col.repository"), T("col.task"), T("col.last"), T("col.agents")}
}

// The sessions table's columns: their titles, the least width each may be
// dragged to, and the width each starts at, in logical pixels.
var (
	ColMin     = []int{40, 50, 80, 50, 60, 60, 30, 44}
	ColDefault = []int{56, 72, 104, 68, 96, 130, 40, 56}
)

// colContext is the index of the Context column, the one that draws a gauge. It
// comes third so the gauge is on screen when the panel opens at its default
// width.
const colContext = 2

// ClampColW is w, a column's width, held between that column's least width and a
// bound that keeps a hostile stored value from making a table of a million
// pixels. An unknown column keeps w as it is.
func ClampColW(col, w int) int {
	if col < 0 || col >= len(ColMin) {
		return w
	}
	return min(max(w, ColMin[col]), maxColW)
}

// SetColWidth is the owner dragging column col to w logical pixels: the width is
// held at the column's minimum and recorded; a column that does not exist is
// ignored.
func (vm *ViewModel) SetColWidth(col, w int) {
	if col < 0 || col >= vm.ColW.Len() {
		return
	}
	w = ClampColW(col, w)
	if vm.ColW.At(col) != w {
		vm.ColW.Set(col, w)
	}
}

// Store is where a viewer's layout is kept between visits: the browser's
// localStorage, through the host. A host whose storage is unavailable passes
// none, or one that does nothing: the layout is then the default each time.
type Store interface {
	Get(key string) (value string, ok bool)
	Set(key, value string)
}

// Store keys.
const (
	keyPanelW = "agentscope.panelW"
	keyColW   = "agentscope.colW"
	keySplit  = "agentscope.split"
)

// SetStore restores the viewer's layout from st and keeps it there as it changes.
// What is stored is untrusted (another page of the same origin could have
// written it): it is parsed strictly and clamped, and anything else is ignored.
func (s *Scene) SetStore(st Store) {
	vm := s.vm
	if v, ok := st.Get(keyPanelW); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			vm.PanelW.Set(n)
		}
	}
	if v, ok := st.Get(keyColW); ok {
		if ws, ok := parseColW(v); ok {
			for i, w := range ws {
				vm.SetColWidth(i, w)
			}
		}
	}
	if v, ok := st.Get(keySplit); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			vm.Split.Set(n)
		}
	}
	s.unbind = append(s.unbind,
		vm.Split.SubscribeChanged(func() { st.Set(keySplit, strconv.Itoa(vm.Split.Get())) }),
		vm.PanelW.SubscribeChanged(func() { st.Set(keyPanelW, strconv.Itoa(vm.PanelW.Get())) }),
		vm.ColW.SubscribeChanged(func() { st.Set(keyColW, formatColW(vm.ColW.Slice())) }),
	)
}

// parseColW reads "48,62,..." with exactly one width per column.
func parseColW(v string) ([]int, bool) {
	parts := strings.Split(v, ",")
	if len(parts) != len(ColMin) {
		return nil, false
	}
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}

func formatColW(ws []int) string {
	parts := make([]string, len(ws))
	for i, w := range ws {
		parts[i] = strconv.Itoa(w)
	}
	return strings.Join(parts, ",")
}
