package ui

import (
	"github.com/go-widgets/mvvm"
	"github.com/go-widgets/mvvmtk"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/toolkit/iconoir"
)

// The layout constants are logical pixels: the Scene multiplies them by the
// device pixel ratio it is given (Resize), so one layout serves a 1x screen and
// a 2x one. The canvas itself is as large as the browser's viewport.
const (
	margin    = 16
	gap       = 12
	headerH   = 56
	connW     = 104
	chipW     = 104
	searchW   = 280
	expiredW  = 150
	toggleW   = 96
	iconW     = 30
	titleW    = 120
	panelW    = 420 // the side panel's width, when it fits
	cardPad   = 12
	nodeRound = 6
	controlsH = 30
	radioW    = 96
)

// DefaultW and DefaultH are the size a Scene has before the host tells it the
// viewport's.
const (
	DefaultW = 1280
	DefaultH = 800
)

// Brand is agentscope's accent: the go-authn account page's brand colour.
var Brand = toolkit.RGB(0x00, 0x79, 0xA8)

// Scene is the View: the widgets, their layout, and the bindings to the
// ViewModel. It never assigns a widget's state field itself, and draws nothing
// by hand: every surface is a toolkit widget (Frame cards, Chips, the graph, the
// charts, the side panel in a toolkit Overlay).
type Scene struct {
	w, h  int
	scale float64
	theme *toolkit.Theme
	vm    *ViewModel

	search *toolkit.SearchEntry
	// expired is the "show expired leases" toggle.
	expired *toolkit.CheckButton
	toggle  *toolkit.Button // opens and closes the side panel
	tabs    *toolkit.Notebook
	title   *toolkit.Label
	logo    *toolkit.StatusIcon
	conn    *toolkit.Chip // the connection-state chip
	models  [ModelChipSlots]*toolkit.Chip
	graph   *toolkit.GraphView
	table   *toolkit.Table
	detail  *toolkit.ListBox
	status  *toolkit.Label

	// The Quota tab.
	spanGroup, kindGroup *toolkit.RadioGroup
	spanRadios           []*toolkit.RadioButton
	kindRadios           []*toolkit.RadioButton
	official, cum        *toolkit.TimeSeriesChart
	weekPct              *toolkit.TimeSeriesChart
	outBars, cacheBars   *toolkit.StackedBarChart
	weekBars             *toolkit.StackedBarChart
	notes                *toolkit.Label

	// Rebuilt by layout.
	overlay *toolkit.Overlay
	root    *toolkit.VBox
	pages   *toolkit.Stack
	quota   *toolkit.Stack // the Quota tab's two layouts
	panel   *toolkit.Frame

	keyTarget  toolkit.Widget
	dragTarget toolkit.Widget
	dragBounds toolkit.Rect

	dirty  bool
	unbind []func()
}

// NewScene builds the View over a fresh ViewModel, at 1x.
func NewScene(w, h int) *Scene {
	_ = toolkit.UseOpenTypeText()
	vm := NewViewModel()
	s := &Scene{w: w, h: h, scale: 1, theme: toolkit.Slate(Brand, false), vm: vm}

	s.search = toolkit.NewSearchEntry("")
	// Without an Icon the entry draws "?" as a stand-in for the magnifier.
	s.search.Icon = toolkit.DrawIconSearch
	s.expired = toolkit.NewCheckButton("expired leases", false)
	s.toggle = toolkit.NewButton("Sessions (t)", nil)
	s.tabs = toolkit.NewNotebook()
	s.tabs.AddTab("Graph", nil)
	s.tabs.AddTab("Quota", nil)
	s.title = toolkit.NewLabel("agentscope")
	s.logo = toolkit.NewStatusIcon(iconoir.Drawer("network-right"))
	s.conn = toolkit.NewChip("")
	s.conn.Style = toolkit.ChipBrand
	for i := range s.models {
		s.models[i] = toolkit.NewChip("")
		s.models[i].Style = toolkit.ChipPlain
	}

	s.graph = toolkit.NewGraphView(vm.Graph)
	s.graph.ShowLegend = true
	// Enter, or a second click on a selected node, activates it: an org node
	// expands or collapses (the expansion is ViewModel state).
	s.graph.OnActivate = vm.ToggleOrg
	s.table = toolkit.NewTable(tableColumns(1), nil)
	// A click selects a row only while MultiSelect is on (table.go OnEvent
	// returns before selecting otherwise; go-widgets/toolkit#595); a plain
	// click still selects one row.
	s.table.MultiSelect = true
	s.detail = toolkit.NewListBox(nil)
	s.status = toolkit.NewLabel("")

	s.buildQuotaWidgets()
	s.bind()
	s.applyTheme()
	s.layout()
	s.dirty = true
	return s
}

// buildQuotaWidgets makes the Quota tab's widgets; their data and bindings come
// from bindQuota.
func (s *Scene) buildQuotaWidgets() {
	s.spanGroup, s.kindGroup = toolkit.NewRadioGroup(), toolkit.NewRadioGroup()
	for _, n := range SpanNames {
		r := toolkit.NewRadioButton(n)
		s.spanGroup.Add(r)
		s.spanRadios = append(s.spanRadios, r)
	}
	for _, n := range KindNames {
		r := toolkit.NewRadioButton(n)
		s.kindGroup.Add(r)
		s.kindRadios = append(s.kindRadios, r)
	}
	line := func() *toolkit.TimeSeriesChart { return toolkit.NewTimeSeriesChart(nil, 0, 100) }
	s.official, s.cum, s.weekPct = line(), line(), line()
	stack := func() *toolkit.StackedBarChart { return toolkit.NewStackedBarChart(nil, nil) }
	s.outBars, s.cacheBars, s.weekBars = stack(), stack(), stack()
	s.notes = toolkit.NewLabel("")
}

// bind connects the widgets to the ViewModel; every observable it subscribes to
// invalidates the scene.
func (s *Scene) bind() {
	vm := s.vm
	invalidate := func() { s.dirty = true }
	s.unbind = append(s.unbind,
		mvvmtk.BindText(s.search, vm.Query, invalidate),
		bindChip(s.conn, vm.Conn, invalidate),
		mvvmtk.BindChecked(s.expired, vm.ShowExpired, invalidate),
		mvvmtk.BindCommand(s.toggle, vm.TogglePanel, invalidate),
		mvvmtk.BindNotebook(s.tabs, vm.Tab, invalidate),
		vm.Expanded.SubscribeChanged(invalidate),
		mvvmtk.BindLabel(s.status, vm.Status, invalidate),
		mvvmtk.BindLabel(s.notes, vm.QuotaNotes, invalidate),
		mvvmtk.BindTableSelection(s.table, vm.TableSel, invalidate),
		mvvmtk.BindListItems(s.detail, vm.Detail, func(l string) string { return l }, invalidate),
		mvvmtk.BindTableRows(s.table, vm.Rows, Row.Cells, invalidate),
		mvvmtk.BindRadioGroup(s.spanGroup, vm.Span, invalidate),
		mvvmtk.BindRadioGroup(s.kindGroup, vm.Kind, invalidate),
		vm.Graph.Selected.SubscribeChanged(invalidate),
		vm.Graph.Filter.SubscribeChanged(invalidate),
		vm.Graph.Edges.SubscribeChanged(invalidate),
		vm.Graph.Nodes.SubscribeChanged(invalidate),
		vm.Graph.Clusters.SubscribeChanged(invalidate),
		vm.Dark.SubscribeChanged(func() { s.applyTheme(); s.refreshQuota(); invalidate() }),
		vm.PanelOpen.SubscribeChanged(func() { s.syncPanel(); invalidate() }),
		vm.Tab.SubscribeChanged(func() { s.syncPages(); s.syncPanel(); invalidate() }),
		vm.Span.SubscribeChanged(func() { s.syncPages(); s.refreshQuota(); invalidate() }),
		vm.Kind.SubscribeChanged(func() { s.refreshQuota(); invalidate() }),
		vm.Usage.SubscribeChanged(func() { s.refreshQuota(); invalidate() }),
	)
	for i, c := range s.models {
		s.unbind = append(s.unbind, bindChip(c, vm.ModelChips[i], invalidate))
	}
	// The groups start on the ViewModel's values.
	s.spanGroup.Active().Set(vm.Span.Get())
	s.kindGroup.Active().Set(vm.Kind.Get())
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

// px is a logical length in device pixels.
func (s *Scene) px(v int) int { return int(float64(v)*s.scale + 0.5) }

// Resize makes the scene w by h device pixels at the given device pixel ratio
// and lays it out again: the graph takes every pixel the header and the tabs do
// not, and its clusters flow into as many columns as the width holds. A resize
// may re-lay-out the graph; an edge update never does.
func (s *Scene) Resize(w, h int, scale float64) {
	if w < 1 || h < 1 || scale <= 0 {
		return
	}
	s.w, s.h, s.scale = w, h, scale
	s.layout()
	s.dirty = true
}

// Size is the scene's size in device pixels.
func (s *Scene) Size() (w, h int) { return s.w, s.h }

// layout builds the containers out of the (persistent) widgets and gives
// everything its bounds.
func (s *Scene) layout() {
	toolkit.SetMetricScale(s.scale)
	s.applyMetrics()
	px := s.px

	// Header card: the logo, the name, the connection chip, the model-count
	// chips, then the search, the toggles and the panel button on the right.
	top := toolkit.NewHBox()
	top.Spacing = px(gap / 2)
	top.AddFixed(s.logo, px(iconW))
	top.AddFixed(s.title, px(titleW))
	top.AddFixed(s.conn, px(connW))
	for _, c := range s.models {
		top.AddFixed(c, px(chipW))
	}
	top.AddFlex(toolkit.NewLabel(""), 1)
	top.AddFixed(s.search, px(searchW))
	top.AddFixed(s.expired, px(expiredW))
	top.AddFixed(s.toggle, px(toggleW))
	header := card("", top, cardPad)

	// The two tabs share the space under the strip.
	s.pages = toolkit.NewStack()
	s.pages.AddPage("graph", card("", s.graph, cardPad/2))
	s.pages.AddPage("quota", s.quotaPage())

	s.root = toolkit.NewVBox()
	s.root.Spacing = px(gap)
	s.root.AddFixed(header, px(headerH))
	s.root.AddFixed(s.tabs, toolkit.TouchTarget(px(toolkit.NotebookTabStripH)))
	s.root.AddFlex(s.pages, 1)
	s.root.AddFixed(s.status, px(toolkit.StatusbarH))

	// The side panel floats over the graph, right-aligned under the strip: the
	// sessions table over the details, in a card of its own.
	side := toolkit.NewVBox()
	side.Spacing = px(gap)
	side.AddFlex(card("Sessions", s.table, cardPad), 1)
	side.AddFlex(card("Details", s.detail, cardPad), 1)
	s.panel = card("", side, cardPad)

	s.overlay = toolkit.NewOverlay(s.root)
	s.overlay.SetBounds(toolkit.Rect{W: s.w, H: s.h})
	s.root.SetBounds(toolkit.Rect{X: px(margin), Y: px(margin), W: s.w - 2*px(margin), H: s.h - 2*px(margin)})
	b := s.pages.Bounds()
	pw := min(px(panelW), b.W*2/3)
	s.panel.SetBounds(toolkit.Rect{X: b.X + b.W - pw, Y: b.Y, W: pw, H: b.H})
	s.syncPages()
	s.syncPanel()
	s.refreshQuota()
}

// quotaPage is the Quota tab: the span and kind selectors over the charts, and
// the notes under them. The "range" layout serves the 5 h and 7 day spans, the
// "weeks" layout the 4 x 7 days one.
func (s *Scene) quotaPage() toolkit.Widget {
	px := s.px
	controls := toolkit.NewHBox()
	controls.Spacing = px(gap / 2)
	controls.AddFixed(toolkit.NewLabel("span"), px(48))
	for _, r := range s.spanRadios {
		controls.AddFixed(r, px(radioW))
	}
	controls.AddFixed(toolkit.NewLabel(""), px(gap))
	controls.AddFixed(toolkit.NewLabel("tokens (4 x 7 days)"), px(150))
	for _, r := range s.kindRadios {
		controls.AddFixed(r, px(radioW))
	}
	controls.AddFlex(toolkit.NewLabel(""), 1)

	tokens := toolkit.NewHBox()
	tokens.Spacing = px(gap)
	tokens.AddFlex(card("Output tokens", s.outBars, cardPad), 1)
	tokens.AddFlex(card("Cache-read tokens", s.cacheBars, cardPad), 1)
	rng := toolkit.NewVBox()
	rng.Spacing = px(gap)
	rng.AddFlex(card("Official usage (% of each window)", s.official, cardPad), 3)
	rng.AddFlex(tokens, 2)

	upper := toolkit.NewHBox()
	upper.Spacing = px(gap)
	upper.AddFlex(card("Tokens per week", s.weekBars, cardPad), 1)
	upper.AddFlex(card("Cumulative tokens by day of the week", s.cum, cardPad), 1)
	wk := toolkit.NewVBox()
	wk.Spacing = px(gap)
	wk.AddFlex(upper, 3)
	wk.AddFlex(card("Official weekly % (recorded)", s.weekPct, cardPad), 2)

	s.quota = toolkit.NewStack()
	s.quota.AddPage("range", rng)
	s.quota.AddPage("weeks", wk)

	page := toolkit.NewVBox()
	page.Spacing = px(gap)
	page.AddFixed(controls, px(controlsH))
	page.AddFlex(s.quota, 1)
	page.AddFixed(s.notes, px(toolkit.StatusbarH))
	return page
}

// syncPages shows the page of the tab, and the layout of the span.
func (s *Scene) syncPages() {
	s.pages.SetVisible([]string{"graph", "quota"}[min(max(s.vm.Tab.Get(), 0), 1)])
	if s.vm.Span.Get() == SpanWeeks {
		s.quota.SetVisible("weeks")
	} else {
		s.quota.SetVisible("range")
	}
	// The page that was hidden has no bounds yet.
	s.pages.SetBounds(s.pages.Bounds())
}

// PanelVisible is whether the side panel is on screen: open, over the graph.
func (s *Scene) PanelVisible() bool {
	return s.vm.PanelOpen.Get() && s.vm.Tab.Get() == TabGraph
}

// syncPanel puts the panel in the overlay, or takes it out.
func (s *Scene) syncPanel() {
	s.overlay.Clear()
	if s.PanelVisible() {
		s.overlay.Push(s.panel)
	}
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
	s.overlay.Draw(p, s.theme)
}

// targets are the widgets that take a click, topmost first: the panel's over the
// graph's, then the header's, then those of the shown tab.
func (s *Scene) targets() []toolkit.Widget {
	var t []toolkit.Widget
	if s.PanelVisible() {
		t = append(t, s.table, s.detail)
	}
	t = append(t, s.search, s.expired, s.toggle, s.tabs)
	if s.vm.Tab.Get() == TabGraph {
		return append(t, s.graph)
	}
	for _, r := range s.spanRadios {
		t = append(t, r)
	}
	if s.vm.Span.Get() == SpanWeeks {
		for _, r := range s.kindRadios {
			t = append(t, r)
		}
	}
	return t
}

// HandleClick dispatches a click at surface (x, y) to the widget under it. A
// click on the open panel never reaches the graph beneath it.
func (s *Scene) HandleClick(x, y int) bool {
	for _, w := range s.targets() {
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

// HandleScroll routes a wheel scroll (Delta in rows, positive = down) to the
// scrollable under the pointer.
func (s *Scene) HandleScroll(x, y, delta int) bool {
	e := toolkit.Event{Kind: toolkit.EventScroll, X: x, Y: y, Delta: delta}
	for _, w := range s.targets() {
		if r := w.Bounds(); inside(x, y, r) {
			w.OnEvent(local(e, r))
			return true
		}
	}
	return true
}

// HandleChar routes a printable character to the focused widget; "t" outside the
// search box toggles the side panel.
func (s *Scene) HandleChar(ch string) bool {
	if ch == "t" && s.keyTarget != toolkit.Widget(s.search) {
		s.vm.TogglePanel.Execute()
		return true
	}
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
