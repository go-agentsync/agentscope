package ui

import (
	"github.com/go-widgets/mvvm"
	"github.com/go-widgets/mvvmtk"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/toolkit/iconoir"
)

// Surface is the canvas size. Fixed: the graph, the table and the detail pane
// each sit in a fixed card and scroll or clip inside it.
const (
	SurfaceW = 1680
	SurfaceH = 940
)

const (
	margin    = 16
	gap       = 12
	headerH   = 56
	connW     = 104
	chipW     = 104
	searchW   = 360
	expiredW  = 150
	iconW     = 30
	titleW    = 120
	rightW    = 720
	cardPad   = 12
	nodeRound = 6
)

// Brand is agentscope's accent: the go-authn account page's brand colour.
var Brand = toolkit.RGB(0x00, 0x79, 0xA8)

// Scene is the View: the widgets, their layout, and the bindings to the
// ViewModel. It never assigns a widget's state field itself, and draws nothing
// by hand: every surface is a toolkit widget (Frame cards, Chips, the graph).
type Scene struct {
	w, h  int
	theme *toolkit.Theme
	vm    *ViewModel

	search *toolkit.SearchEntry
	// expired is the "show expired leases" toggle.
	expired *toolkit.CheckButton
	title   *toolkit.Label
	conn    *toolkit.Chip // the connection-state chip
	models  [ModelChipSlots]*toolkit.Chip
	graph   *toolkit.GraphView
	table   *toolkit.Table
	detail  *toolkit.ListBox
	status  *toolkit.Label

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
	s := &Scene{w: w, h: h, theme: toolkit.Slate(Brand, false), vm: vm}
	invalidate := func() { s.dirty = true }

	s.search = toolkit.NewSearchEntry("")
	// Without an Icon the entry draws "?" as a stand-in for the magnifier.
	s.search.Icon = toolkit.DrawIconSearch
	s.expired = toolkit.NewCheckButton("expired leases", false)
	s.title = toolkit.NewLabel("agentscope")
	s.title.FontSize = 20
	logo := toolkit.NewStatusIcon(iconoir.Drawer("network-right"))
	connChip := toolkit.NewChip("")
	connChip.Style = toolkit.ChipBrand
	s.conn = connChip
	modelChips := &s.models
	for i := range modelChips {
		modelChips[i] = toolkit.NewChip("")
		modelChips[i].Style = toolkit.ChipPlain
	}

	s.graph = toolkit.NewGraphView(vm.Graph)
	s.graph.ShowLegend = true
	s.graph.NodeWidth = NodeWidth // wide enough for an org/repo or a task
	s.graph.CornerRadius = nodeRound
	// Enter, or a second click on a selected node, activates it: an org node
	// expands or collapses (the expansion is ViewModel state).
	s.graph.OnActivate = vm.ToggleOrg
	s.table = toolkit.NewTable([]toolkit.TableColumn{
		{Title: "State", Width: 58},
		{Title: "Session", Width: 78},
		{Title: "Model", Width: 74},
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
		bindChip(connChip, vm.Conn, invalidate),
		mvvmtk.BindChecked(s.expired, vm.ShowExpired, invalidate),
		vm.Expanded.SubscribeChanged(invalidate),
		mvvmtk.BindLabel(s.status, vm.Status, invalidate),
		mvvmtk.BindTableSelection(s.table, vm.TableSel, invalidate),
		mvvmtk.BindListItems(s.detail, vm.Detail, func(l string) string { return l }, invalidate),
		mvvmtk.BindTableRows(s.table, vm.Rows, Row.Cells, invalidate),
		vm.Graph.Selected.SubscribeChanged(invalidate),
		vm.Graph.Filter.SubscribeChanged(invalidate),
		vm.Graph.Edges.SubscribeChanged(invalidate),
		vm.Graph.Nodes.SubscribeChanged(invalidate),
		vm.Graph.Clusters.SubscribeChanged(invalidate),
		vm.Dark.SubscribeChanged(func() { s.applyTheme(); invalidate() }),
	)
	for i, c := range modelChips {
		s.unbind = append(s.unbind, bindChip(c, vm.ModelChips[i], invalidate))
	}

	// Header card: the logo, the name, the connection chip, the model-count
	// chips, then the search and the toggle on the right.
	top := toolkit.NewHBox()
	top.Spacing = gap / 2
	top.AddFixed(logo, iconW)
	top.AddFixed(s.title, titleW)
	top.AddFixed(connChip, connW)
	for _, c := range modelChips {
		top.AddFixed(c, chipW)
	}
	top.AddFlex(toolkit.NewLabel(""), 1)
	top.AddFixed(s.search, searchW)
	top.AddFixed(s.expired, expiredW)
	header := card("", top, cardPad)

	graphCard := card("", s.graph, cardPad/2)
	tableCard := card("Sessions", s.table, cardPad)
	detailCard := card("Details", s.detail, cardPad)

	side := toolkit.NewVBox()
	side.Spacing = gap
	side.AddFlex(tableCard, 1)
	side.AddFlex(detailCard, 1)

	body := toolkit.NewHBox()
	body.Spacing = gap
	body.AddFlex(graphCard, 1)
	body.AddFixed(side, rightW)

	s.root = toolkit.NewVBox()
	s.root.Spacing = gap
	s.root.AddFixed(header, headerH)
	s.root.AddFlex(body, 1)
	s.root.AddFixed(s.status, toolkit.StatusbarH)
	s.root.SetBounds(toolkit.Rect{X: margin, Y: margin, W: w - 2*margin, H: h - 2*margin})

	s.clickables = []toolkit.Widget{s.search, s.expired, s.graph, s.table, s.detail}
	s.applyTheme()
	s.dirty = true
	return s
}

// bindChip one-way-binds a chip's text to an observable through mvvm.BindField
// (a chip has no edit callback; BindField wants a hook slot, so it gets an unused
// one). mvvmtk has no chip binder yet.
func bindChip(c *toolkit.Chip, obs *mvvm.Observable[string], invalidate func()) (unbind func()) {
	var unused func(string)
	return mvvm.BindField(obs, &c.Text, &unused, invalidate)
}

// card wraps child in a card: a rounded surface with a soft border and shadow
// and, with a title, a heading over a hairline.
func card(title string, child toolkit.Widget, pad int) *toolkit.Frame {
	f := toolkit.NewFrame(child)
	f.Card = true
	f.Title = title
	f.Padding = pad
	return f
}

// applyTheme sets the Slate theme of the system's preference, and what depends
// on it: the graph's category palette and the title's brand ink.
func (s *Scene) applyTheme() {
	dark := s.vm.Dark.Get()
	s.theme = toolkit.Slate(Brand, dark)
	s.graph.Palette = Palette(dark)
	s.title.Ink = s.theme.Extra["brand_ink"]
}

// Palette is the graph's category palette: the Okabe-Ito order, which is safe
// under the common colour-vision deficiencies, with its lightness tuned to the
// page: eased toward the Slate ground in light, lifted a little in dark so a pill
// keeps its contrast against the dark card. The order is what the legend and the
// colour assignment (by first appearance) rely on, so it is never reordered.
func Palette(dark bool) []toolkit.RGBA {
	base := []toolkit.RGBA{
		toolkit.RGB(0xE6, 0x9F, 0x00), toolkit.RGB(0x56, 0xB4, 0xE9), toolkit.RGB(0x00, 0x9E, 0x73), toolkit.RGB(0xF0, 0xE4, 0x42),
		toolkit.RGB(0x00, 0x72, 0xB2), toolkit.RGB(0xD5, 0x5E, 0x00), toolkit.RGB(0xCC, 0x79, 0xA7), toolkit.RGB(0x99, 0x99, 0x99),
	}
	out := make([]toolkit.RGBA, len(base))
	for i, c := range base {
		if dark {
			out[i] = mix(c, toolkit.RGB(0xFF, 0xFF, 0xFF), 0.12)
		} else {
			out[i] = mix(c, toolkit.RGB(0x0F, 0x17, 0x2A), 0.06)
		}
	}
	return out
}

// mix is c moved toward to by t (0..1).
func mix(c, to toolkit.RGBA, t float64) toolkit.RGBA {
	f := func(a, b uint8) uint8 { return uint8(float64(a)*(1-t) + float64(b)*t + 0.5) }
	return toolkit.RGBA{R: f(c.R, to.R), G: f(c.G, to.G), B: f(c.B, to.B), A: 0xFF}
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
	if code == "Escape" {
		s.vm.CollapseAll() // Escape folds every open org (the graph also clears its selection)
	}
	if s.keyTarget == nil {
		return code == "Escape"
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
