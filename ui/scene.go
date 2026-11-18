package ui

import (
	"github.com/go-widgets/mvvmtk"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// Surface is the canvas size. Fixed: the graph, the table and the detail pane
// each sit in a fixed box and scroll or clip inside it.
const (
	SurfaceW = 1680
	SurfaceH = 940
)

const (
	margin   = 8
	gap      = 6
	searchH  = 26
	connW    = 150
	rightW   = 640
	tableMin = 1 // flex weight of the table
	detailFx = 1 // flex weight of the detail pane
)

// Scene is the View: the widgets, their layout, and the bindings to the
// ViewModel. It never assigns a widget's state field itself.
type Scene struct {
	w, h  int
	theme *toolkit.Theme
	vm    *ViewModel

	search *toolkit.SearchEntry
	conn   *toolkit.Label
	graph  *toolkit.GraphView
	table  *toolkit.Table
	detail *toolkit.ListBox
	status *toolkit.Label

	root *toolkit.VBox

	clickables []toolkit.Widget
	keyTarget  toolkit.Widget
	dragTarget toolkit.Widget
	dragBounds toolkit.Rect

	dirty  bool
	unbind []func()
}

// NewScene builds the View over a fresh ViewModel.
func NewScene(w, h int) *Scene {
	_ = toolkit.UseOpenTypeText()
	vm := NewViewModel()
	s := &Scene{w: w, h: h, theme: toolkit.DefaultLight(), vm: vm}
	invalidate := func() { s.dirty = true }

	s.search = toolkit.NewSearchEntry("")
	// Without an Icon the entry draws "?" as a stand-in for the magnifier.
	s.search.Icon = toolkit.DrawIconSearch
	s.conn = toolkit.NewLabel("")
	s.graph = toolkit.NewGraphView(vm.Graph)
	s.graph.ShowLegend = true
	s.graph.NodeWidth = NodeWidth // wide enough for an org/repo or a task
	s.table = toolkit.NewTable([]toolkit.TableColumn{
		{Title: "State", Width: 58},
		{Title: "Session", Width: 78},
		{Title: "Repository", Width: 176},
		{Title: "Task"},
		{Title: "Last", Width: 40},
		{Title: "Ag", Width: 28},
	}, nil)
	// A click selects a row only while MultiSelect is on (table.go OnEvent
	// returns before selecting otherwise; go-widgets/toolkit#595); a plain
	// click still selects one row.
	s.table.MultiSelect = true
	s.detail = toolkit.NewListBox(nil)
	s.status = toolkit.NewLabel("")

	s.unbind = append(s.unbind,
		mvvmtk.BindText(s.search, vm.Query, invalidate),
		mvvmtk.BindLabel(s.conn, vm.Conn, invalidate),
		mvvmtk.BindLabel(s.status, vm.Status, invalidate),
		mvvmtk.BindTableSelection(s.table, vm.TableSel, invalidate),
		mvvmtk.BindListItems(s.detail, vm.Detail, func(l string) string { return l }, invalidate),
		mvvmtk.BindTableRows(s.table, vm.Rows, Row.Cells, invalidate),
		vm.Graph.Selected.SubscribeChanged(invalidate),
		vm.Graph.Filter.SubscribeChanged(invalidate),
		vm.Graph.Edges.SubscribeChanged(invalidate),
		vm.Graph.Nodes.SubscribeChanged(invalidate),
		vm.Graph.Clusters.SubscribeChanged(invalidate),
	)

	top := toolkit.NewHBox()
	top.Spacing = gap
	top.AddFlex(s.search, 1)
	top.AddFixed(s.conn, connW)

	side := toolkit.NewVBox()
	side.Spacing = gap
	side.AddFlex(s.table, tableMin)
	side.AddFlex(s.detail, detailFx)

	body := toolkit.NewHBox()
	body.Spacing = gap
	body.AddFlex(s.graph, 1)
	body.AddFixed(side, rightW)

	s.root = toolkit.NewVBox()
	s.root.Spacing = gap
	s.root.AddFixed(top, searchH)
	s.root.AddFlex(body, 1)
	s.root.AddFixed(s.status, toolkit.StatusbarH)
	s.root.SetBounds(toolkit.Rect{X: margin, Y: margin, W: w - 2*margin, H: h - 2*margin})

	s.clickables = []toolkit.Widget{s.search, s.graph, s.table, s.detail}
	s.dirty = true
	return s
}

// VM is the ViewModel the host feeds snapshots to.
func (s *Scene) VM() *ViewModel { return s.vm }

// Dirty reports, and clears, the repaint-needed flag.
func (s *Scene) Dirty() bool {
	d := s.dirty
	s.dirty = false
	return d
}

// Draw paints the scene onto buf, an RGBA row-major slice of w*h pixels.
func (s *Scene) Draw(buf []byte) {
	fillBG(buf, s.theme.Background)
	p := painter.NewPixelPainter(buf, s.w, s.h)
	s.root.Draw(p, s.theme)
}

// HandleClick dispatches a click at surface (x, y) to the widget under it.
func (s *Scene) HandleClick(x, y int) bool {
	for _, w := range s.clickables {
		r := w.Bounds()
		if inside(x, y, r) {
			s.dragTarget, s.dragBounds = w, r
			s.keyTarget = w
			s.search.SetFocused(w == toolkit.Widget(s.search))
			w.OnEvent(local(toolkit.Event{Kind: toolkit.EventClick, X: x, Y: y}, r))
			return true
		}
	}
	s.dragTarget, s.keyTarget = nil, nil
	s.search.SetFocused(false)
	return true
}

// HandleScroll routes a wheel scroll (Delta in rows, positive = down).
func (s *Scene) HandleScroll(x, y, delta int) bool {
	e := toolkit.Event{Kind: toolkit.EventScroll, X: x, Y: y, Delta: delta}
	s.root.OnEvent(local(e, s.root.Bounds()))
	return true
}

// HandleChar routes a printable character to the focused widget.
func (s *Scene) HandleChar(ch string) bool {
	if s.keyTarget == nil {
		return false
	}
	s.keyTarget.OnEvent(toolkit.Event{Kind: toolkit.EventChar, Code: ch})
	return true
}

// HandleKeyDown routes a named key to the focused widget.
func (s *Scene) HandleKeyDown(code string) bool {
	if s.keyTarget == nil {
		return false
	}
	s.keyTarget.OnEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: code})
	return true
}

// HandleMove routes pointer motion to the widget grabbed on press.
func (s *Scene) HandleMove(x, y int) bool {
	if s.dragTarget == nil {
		return false
	}
	s.dragTarget.OnEvent(toolkit.Event{Kind: toolkit.EventMouseDrag, X: x - s.dragBounds.X, Y: y - s.dragBounds.Y})
	return true
}

// HandleRelease routes a button release to the grabbed widget and drops it.
func (s *Scene) HandleRelease(x, y int) bool {
	if s.dragTarget == nil {
		return false
	}
	s.dragTarget.OnEvent(toolkit.Event{Kind: toolkit.EventMouseUp, X: x - s.dragBounds.X, Y: y - s.dragBounds.Y})
	s.dragTarget = nil
	return true
}

func fillBG(buf []byte, c toolkit.RGBA) {
	for i := 0; i+3 < len(buf); i += 4 {
		buf[i], buf[i+1], buf[i+2], buf[i+3] = c.R, c.G, c.B, c.A
	}
}

func inside(x, y int, r toolkit.Rect) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

func local(e toolkit.Event, r toolkit.Rect) toolkit.Event {
	e.X -= r.X
	e.Y -= r.Y
	return e
}
