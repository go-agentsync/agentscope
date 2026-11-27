package ui

import (
	"time"

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
	detail  *toolkit.WrapList
	status  *toolkit.Label

	// The side panel: a top bar with its title and a close button, the filter row,
	// the sessions table, and the Details gauges and text. split holds the panel
	// beside a spacer so its left edge can be dragged.
	themeDD, langDD         *toolkit.DropDown // the header's theme and language selectors
	renameBody, colorBody   *toolkit.Label    // the dialogs' lines
	panelTitle              *toolkit.Label
	closeBtn                *toolkit.IconButton
	stateDD, lastDD, repoDD *toolkit.DropDown
	modelChecks             [ModelFilterSlots]*toolkit.CheckButton
	clearBtn                *toolkit.Button
	ctxTable                *toolkit.Table
	split                   *toolkit.Splitter
	spacer                  *toolkit.Label
	panelPane               *toolkit.SplitPane
	hover                   int               // the splitter divider under the pointer, -1 for none
	vsplit                  *toolkit.Splitter // Sessions over Details, inside the panel
	vhover                  int               // the divider of vsplit under the pointer, -1 for none
	panelFocus              []toolkit.Widget
	dockUnsub, splitUnsub   []func()
	quotaStale              bool // the usage changed while the Quota tab was not shown

	// The Rename action of the accounts table, and its dialog.
	renameBtn   *toolkit.Button
	renameDlg   *toolkit.Dialog
	renameEntry *toolkit.Entry
	renameErr   *toolkit.Label
	renameOK    *toolkit.Button
	renameNo    *toolkit.Button

	// The Colour action of the accounts table, and its dialog.
	colorBtn    *toolkit.Button
	colorDlg    *toolkit.Dialog
	colorPicker *toolkit.ColorPicker
	colorErr    *toolkit.Label
	colorOK     *toolkit.Button
	colorNo     *toolkit.Button
	colorReset  *toolkit.Button

	// The Quota tab.
	spanGroup, kindGroup *toolkit.RadioGroup
	spanRadios           []*toolkit.RadioButton
	kindRadios           []*toolkit.RadioButton
	official, cum        *toolkit.TimeSeriesChart
	weekPct              *toolkit.TimeSeriesChart
	outBars, cacheBars   *toolkit.StackedBarChart
	weekBars             *toolkit.StackedBarChart
	accountDD            *toolkit.DropDown
	capTable             *toolkit.Table
	host                 *toolkit.PopoverHost
	notes                *toolkit.Label

	// The Guardrails tab: the recurrence and refusal charts, the lessons table with
	// its decision filter, the detail of the selected lesson, and the chips.
	gRec      *toolkit.TimeSeriesChart
	gBars     *toolkit.StackedBarChart
	gTable    *toolkit.Table
	gDetail   *toolkit.WrapList
	gDecision *toolkit.DropDown
	gChips    [GuardChipSlots]*toolkit.Chip
	gNotes    *toolkit.Label

	// Rebuilt by layout.
	overlay *toolkit.Overlay
	root    *toolkit.VBox
	pages   *toolkit.Stack
	quota   *toolkit.Stack // the Quota tab's two layouts
	panel   *toolkit.Frame

	keyTarget toolkit.Widget
	ptr       toolkit.PointerRouter // routes the mouse: local coordinates, the grab, the dividers

	// clock is the time the splitter's double-click detection reads (time.Now,
	// unless a test sets another).
	clock func() time.Time

	dirty  bool
	unbind []func()
}

// NewScene builds the View over a fresh ViewModel, at 1x.
func NewScene(w, h int) *Scene {
	_ = toolkit.UseOpenTypeText()
	vm := NewViewModel()
	s := &Scene{w: w, h: h, scale: 1, theme: toolkit.Slate(Brand, false), vm: vm, clock: time.Now}

	s.search = toolkit.NewSearchEntry("")
	// Without an Icon the entry draws "?" as a stand-in for the magnifier.
	s.search.Icon = toolkit.DrawIconSearch
	s.expired = toolkit.NewCheckButton("", false)
	s.toggle = toolkit.NewButton("", nil)
	s.tabs = toolkit.NewNotebook()
	s.tabs.AddTab("", nil)
	s.tabs.AddTab("", nil)
	s.tabs.AddTab("", nil)
	s.themeDD = toolkit.NewDropDown(ThemeNames(), 0)
	s.langDD = toolkit.NewDropDown(LangNames(), 0)
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
	s.table = toolkit.NewTable(tableColumns(1, nil), nil)
	// A click selects a row only while MultiSelect is on (table.go OnEvent
	// returns before selecting otherwise; go-widgets/toolkit#595); a plain
	// click still selects one row.
	s.table.MultiSelect = true
	s.detail = toolkit.NewWrapList(nil)
	s.status = toolkit.NewLabel("")
	s.buildPanelWidgets()

	s.buildQuotaWidgets()
	s.buildGuardWidgets()
	s.bind()
	s.applyTheme()
	s.applyLang()
	s.dirty = true
	return s
}

// applyLang sets the text of every widget built once, in the language in use, and
// lays the screen out again (the containers read their titles then). The texts the
// ViewModel holds are rebuilt by the ViewModel itself.
func (s *Scene) applyLang() {
	s.expired.Label = T("hdr.expired")
	s.toggle.Label().Set(T("hdr.sessions"))
	for i, k := range []string{"tab.graph", "tab.quota", "tab.guardrails"} {
		s.tabs.Tabs[i].Label = T(k)
	}
	for i, n := range SpanNames() {
		s.spanRadios[i].Label = n
	}
	for i, n := range KindNames() {
		s.kindRadios[i].Label = n
	}
	for b, k := range map[*toolkit.Button]string{
		s.renameBtn: "btn.rename", s.colorBtn: "btn.colour", s.clearBtn: "filter.clear",
		s.renameOK: "btn.ok", s.renameNo: "btn.cancel", s.colorOK: "btn.ok", s.colorNo: "btn.cancel", s.colorReset: "btn.reset",
	} {
		b.Label().Set(T(k))
	}
	s.renameEntry.Placeholder = T("rename.placeholder")
	s.renameDlg.Title, s.colorDlg.Title = T("rename.title"), T("colour.title")
	s.renameBody.Text().Set(T("rename.body"))
	s.colorBody.Text().Set(T("colour.body"))
	s.panelTitle.Text().Set(T("panel.title"))
	s.layout()
}

// buildQuotaWidgets makes the Quota tab's widgets; their data and bindings come
// from bindQuota.
func (s *Scene) buildQuotaWidgets() {
	s.spanGroup, s.kindGroup = toolkit.NewRadioGroup(), toolkit.NewRadioGroup()
	for _, n := range SpanNames() {
		r := toolkit.NewRadioButton(n)
		s.spanGroup.Add(r)
		s.spanRadios = append(s.spanRadios, r)
	}
	for _, n := range KindNames() {
		r := toolkit.NewRadioButton(n)
		s.kindGroup.Add(r)
		s.kindRadios = append(s.kindRadios, r)
	}
	line := func() *toolkit.TimeSeriesChart { return toolkit.NewTimeSeriesChart(nil, 0, 100) }
	s.official, s.cum, s.weekPct = line(), line(), line()
	stack := func() *toolkit.StackedBarChart { return toolkit.NewStackedBarChart(nil, nil) }
	s.outBars, s.cacheBars, s.weekBars = stack(), stack(), stack()
	s.notes = toolkit.NewLabel("")
	s.accountDD = toolkit.NewDropDown([]string{AllAccounts()}, 0)
	s.capTable = toolkit.NewTable(capColumns(1), nil)
	// A click selects a row only while MultiSelect is on (go-widgets/toolkit#595).
	s.capTable.MultiSelect = true
	s.renameBtn = toolkit.NewButton("Rename...", nil)
	s.buildRenameDialog()
	s.colorBtn = toolkit.NewButton("Colour...", nil) // texts are set by applyLang
	s.buildColorDialog()
}

// buildColorDialog makes the Colour dialog: the toolkit's colour picker, a line
// for what went wrong, and Reset, Cancel and OK. Its state is the ViewModel's
// (Color).
func (s *Scene) buildColorDialog() {
	s.colorPicker = toolkit.NewColorPicker(toolkit.RGBA{A: 0xFF})
	s.colorErr = toolkit.NewLabel("")
	s.colorOK = toolkit.NewButton("OK", nil)
	s.colorNo = toolkit.NewButton("Cancel", nil)
	s.colorReset = toolkit.NewButton("Reset", nil)
	body := toolkit.NewVBox()
	body.Spacing = 6
	s.colorBody = toolkit.NewLabel("")
	body.AddFixed(s.colorBody, 22)
	body.AddFlex(s.colorPicker, 1) // the dialog is sized to the picker (openDialog), at the scale in use
	body.AddFixed(s.colorErr, 22)
	s.colorDlg = toolkit.NewDialog("Colour of account", body, s.colorReset, s.colorNo, s.colorOK)
	s.colorDlg.PlainTitle = true
}

// dialogOpen is whether a modal dialog is up: it holds the keyboard and takes
// every click.
func (s *Scene) dialogOpen() bool { return s.vm.Rename.Open.Get() || s.vm.Color.Open.Get() }

// buildRenameDialog makes the Rename dialog: an entry for the name, a line for
// what went wrong, and OK and Cancel. Its state is the ViewModel's (Rename).
func (s *Scene) buildRenameDialog() {
	s.renameEntry = toolkit.NewEntry("")
	s.renameEntry.Placeholder = "display name, e.g. Labo (team)"
	s.renameErr = toolkit.NewLabel("")
	s.renameOK = toolkit.NewButton("OK", nil)
	s.renameNo = toolkit.NewButton("Cancel", nil)
	body := toolkit.NewVBox()
	body.Spacing = 6
	s.renameBody = toolkit.NewLabel("")
	body.AddFixed(s.renameBody, 22)
	body.AddFixed(s.renameErr, 22)
	s.renameDlg = toolkit.NewDialog("Rename account", body, s.renameNo, s.renameOK)
	s.renameDlg.Input = s.renameEntry
	s.renameDlg.PlainTitle = true
}

// buildPanelWidgets makes the side panel's widgets: the top bar, the filter row
// and the gauges of the Details pane.
func (s *Scene) buildPanelWidgets() {
	s.panelTitle = toolkit.NewLabel("Sessions and details")
	s.closeBtn = toolkit.NewIconButton("", nil)
	s.closeBtn.Glyph = iconoir.Drawer("xmark")
	s.closeBtn.Flat = true
	s.stateDD = toolkit.NewDropDown(StateNames(), StateAll)
	s.lastDD = toolkit.NewDropDown(LastNames(), 0)
	s.repoDD = toolkit.NewDropDown([]string{AllRepos()}, 0)
	for i := range s.modelChecks {
		s.modelChecks[i] = toolkit.NewCheckButton("", false)
	}
	s.clearBtn = toolkit.NewButton("Clear filters", nil)
	s.ctxTable = toolkit.NewTable([]toolkit.TableColumn{{Title: T("ctx.of"), Width: 150}, {Title: T("ctx.context")}}, nil)
	s.spacer = toolkit.NewLabel("")
	s.hover, s.vhover = -1, -1
	s.ptr = toolkit.PointerRouter{Edges: s.edges, Targets: s.targets, OnGrab: s.focusOn}
}

// focusOn gives the keyboard to the widget a press is about to reach (nobody for
// a press on nothing); the press itself may then move it again.
func (s *Scene) focusOn(w toolkit.Widget) {
	if s.dialogOpen() { // the dialog holds the keyboard
		return
	}
	s.keyTarget = w
	s.search.SetFocused(w == toolkit.Widget(s.search))
}

// edges are the dividers a press grabs: the one between Sessions and Details
// and the panel's left edge, while the panel is shown and no dialog is open.
func (s *Scene) edges() []toolkit.PointerClaimer {
	if !s.PanelVisible() || s.dialogOpen() {
		return nil
	}
	return []toolkit.PointerClaimer{s.vsplit, s.split}
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
		bindWrapList(s.detail, vm.Detail, invalidate),
		mvvmtk.BindTableRows(s.table, vm.Rows, Row.Cells, invalidate),
		mvvmtk.BindDropDownOptions(s.accountDD, vm.AccountOptions, func(o string) string { return o }, invalidate),
		mvvmtk.BindSelectedIndex(s.accountDD, vm.Account, invalidate),
		mvvmtk.BindTableRows(s.capTable, vm.Capacity, CapRow.Cells, invalidate),
		vm.Account.SubscribeChanged(func() { s.syncPages(); s.refreshQuota(); invalidate() }),
		mvvmtk.BindRadioGroup(s.spanGroup, vm.Span, invalidate),
		mvvmtk.BindRadioGroup(s.kindGroup, vm.Kind, invalidate),
		vm.Graph.Selected.SubscribeChanged(invalidate),
		vm.Graph.Filter.SubscribeChanged(invalidate),
		vm.Graph.Edges.SubscribeChanged(invalidate),
		vm.Graph.Nodes.SubscribeChanged(invalidate),
		vm.Graph.Clusters.SubscribeChanged(invalidate),
		vm.Dark.SubscribeChanged(func() { s.applyTheme(); s.refreshQuota(); s.refreshGuard(); invalidate() }),
		mvvmtk.BindDropDownOptions(s.themeDD, vm.Prefs.ThemeOpts, func(o string) string { return o }, invalidate),
		mvvmtk.BindDropDownOptions(s.langDD, vm.Prefs.LangOpts, func(o string) string { return o }, invalidate),
		mvvmtk.BindDropDownOptions(s.stateDD, vm.Filters.StateOpts, func(o string) string { return o }, invalidate),
		mvvmtk.BindDropDownOptions(s.lastDD, vm.Filters.LastOpts, func(o string) string { return o }, invalidate),
		mvvmtk.BindDropDownOptions(s.gDecision, vm.Guard.DecisionOpts, func(o string) string { return o }, invalidate),
		mvvmtk.BindSelectedIndex(s.themeDD, vm.Prefs.ThemeSel, invalidate),
		mvvmtk.BindSelectedIndex(s.langDD, vm.Prefs.LangSel, invalidate),
		vm.Prefs.Lang.SubscribeChanged(func() { s.applyLang(); invalidate() }),
		vm.PanelOpen.SubscribeChanged(func() { s.syncPanel(); invalidate() }),
		vm.Tab.SubscribeChanged(func() {
			s.syncPages()
			s.syncPanel()
			if s.quotaStale {
				s.refreshQuota()
			}
			invalidate()
		}),
		vm.Span.SubscribeChanged(func() { s.syncPages(); s.refreshQuota(); invalidate() }),
		vm.Kind.SubscribeChanged(func() { s.refreshQuota(); invalidate() }),
		vm.Usage.SubscribeChanged(func() {
			s.refreshQuota()
			if vm.Tab.Get() == TabQuota { // nothing is repainted for a tab that is not shown
				invalidate()
			}
		}),
	)
	s.bindGuard(invalidate)
	f := vm.Filters
	s.unbind = append(s.unbind,
		mvvmtk.BindSelectedIndex(s.stateDD, f.State, invalidate),
		mvvmtk.BindSelectedIndex(s.lastDD, f.Last, invalidate),
		mvvmtk.BindDropDownOptions(s.repoDD, f.RepoOptions, func(o string) string { return o }, invalidate),
		mvvmtk.BindSelectedIndex(s.repoDD, f.Repo, invalidate),
		mvvmtk.BindCommand(s.clearBtn, f.Clear, invalidate),
		mvvmtk.BindTableRows(s.ctxTable, vm.Contexts, CtxRow.Cells, invalidate),
		mvvmtk.BindTableSelection(s.capTable, vm.CapSel, invalidate),
		mvvmtk.BindCommand(s.renameBtn, vm.Rename.Begin, invalidate),
		mvvmtk.BindCommand(s.renameOK, vm.Rename.Confirm, invalidate),
		mvvmtk.BindCommand(s.renameNo, vm.Rename.Cancel, invalidate),
		mvvmtk.BindEntryText(s.renameEntry, vm.Rename.Text, invalidate),
		mvvmtk.BindLabel(s.renameErr, vm.Rename.Err, invalidate),
		vm.Rename.Open.SubscribeChanged(func() { s.syncDialog(); invalidate() }),
		mvvmtk.BindCommand(s.colorBtn, vm.Color.Begin, invalidate),
		mvvmtk.BindCommand(s.colorOK, vm.Color.Confirm, invalidate),
		mvvmtk.BindCommand(s.colorNo, vm.Color.Cancel, invalidate),
		mvvmtk.BindCommand(s.colorReset, vm.Color.Reset, invalidate),
		mvvmtk.BindColorPicker(s.colorPicker, vm.Color.Pick, invalidate),
		mvvmtk.BindLabel(s.colorErr, vm.Color.Err, invalidate),
		vm.Color.Open.SubscribeChanged(func() { s.syncDialog(); invalidate() }),
		vm.PanelW.SubscribeChanged(invalidate), // the splitter lays itself out on its Size
		// A new split decides which filter rows the Sessions pane has room for.
		vm.Split.SubscribeChanged(func() { s.layout(); invalidate() }),
		vm.ColW.SubscribeChanged(func() { s.applyMetrics(); invalidate() }),
	)
	for i := range s.modelChecks {
		s.unbind = append(s.unbind,
			mvvmtk.BindChecked(s.modelChecks[i], f.ModelOn[i], invalidate),
			f.ModelLabels[i].SubscribeChanged(func() { s.layout(); invalidate() }),
		)
	}
	// The close button runs the command the t key does.
	s.closeBtn.OnClick = vm.TogglePanel.Execute
	// The table's column drags are the owner's: the ViewModel holds the widths.
	s.table.OnColumnResize = func(col, w int) { vm.SetColWidth(col, int(float64(w)/s.scale+0.5)) }
	s.table.CellBar = s.contextBar(func(row int) (CtxRow, bool) {
		if row < 0 || row >= vm.Rows.Len() {
			return CtxRow{}, false
		}
		r := vm.Rows.At(row)
		return CtxRow{Text: r.CtxText, Ratio: r.Ratio, Band: r.Band}, r.CtxText != ""
	}, colContext)
	s.ctxTable.CellBar = s.contextBar(func(row int) (CtxRow, bool) {
		if row < 0 || row >= vm.Contexts.Len() {
			return CtxRow{}, false
		}
		return vm.Contexts.At(row), true
	}, 1)
	for i, c := range s.models {
		s.unbind = append(s.unbind, bindChip(c, vm.ModelChips[i], invalidate))
	}
	// The groups start on the ViewModel's values.
	s.spanGroup.Active().Set(vm.Span.Get())
	s.kindGroup.Active().Set(vm.Kind.Get())
}

// contextBar is the Table.CellBar of a gauge column: the row's pressure as a
// fraction of the cell, in the band's colour for the current theme.
func (s *Scene) contextBar(row func(int) (CtxRow, bool), col int) func(r, c int) (float64, toolkit.RGBA, bool) {
	return func(r, c int) (float64, toolkit.RGBA, bool) {
		g, ok := row(r)
		if !ok || c != col {
			return 0, toolkit.RGBA{}, false
		}
		return g.Ratio, PressureColor(g.Band, s.vm.Dark.Get()), true
	}
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
	// The theme and language selectors take room: on a narrow window the name, then
	// the connection chip, give way to them (the status line still says it).
	if s.w >= px(narrowTitleW) {
		top.AddFixed(s.title, px(titleW))
	}
	if s.w >= px(narrowConnW) {
		top.AddFixed(s.conn, px(connW))
	}
	// Under a width the chips do not fit beside the search: they leave the header
	// (the Details and the status line still say it) so nothing runs off its edge.
	if s.w >= px(wideHeaderW) {
		for _, c := range s.models {
			top.AddFixed(c, px(chipW))
		}
	}
	top.AddFlex(s.search, 1) // the search takes what the fixed parts leave
	top.AddFixed(s.expired, px(expiredW))
	top.AddFixed(s.themeDD, px(selectW))
	top.AddFixed(s.langDD, px(selectW))
	top.AddFixed(s.toggle, px(toggleW))
	header := card("", top, cardPad)

	// The two tabs share the space under the strip.
	s.buildPanel()
	s.dock()
	s.pages = toolkit.NewStack()
	s.pages.AddPage("graph", s.split)
	s.pages.AddPage("quota", s.quotaPage())
	s.pages.AddPage("guardrails", s.guardPage())

	s.root = toolkit.NewVBox()
	s.root.Spacing = px(gap)
	s.root.AddFixed(header, px(headerH))
	s.root.AddFixed(s.tabs, toolkit.TouchTarget(px(toolkit.NotebookTabStripH)))
	s.root.AddFlex(s.pages, 1)
	s.root.AddFixed(s.status, px(toolkit.StatusbarH))

	s.overlay = toolkit.NewOverlay(s.root)
	s.host = toolkit.NewPopoverHost(s.overlay)
	s.host.SetBounds(toolkit.Rect{W: s.w, H: s.h})
	s.root.SetBounds(toolkit.Rect{X: px(margin), Y: px(margin), W: s.w - 2*px(margin), H: s.h - 2*px(margin)})
	s.vm.ViewW.Set(int(float64(s.w)/s.scale + 0.5))
	s.hover, s.vhover = -1, -1
	s.syncPages()
	s.syncPanel()
	s.refreshQuota()
	s.refreshGuard()
}

// selectW is the width, in logical pixels, of each of the header's theme and
// language selectors.
const selectW = 96

// narrowTitleW and narrowConnW are the logical widths below which the header drops
// the name, then the connection chip, to keep the selectors on screen.
const (
	narrowTitleW = 900
	narrowConnW  = 700
)

// wideHeaderW is the logical width from which the header has room for the model
// chips beside the search.
const wideHeaderW = 1100

// buildPanel makes the side panel's containers out of its widgets: a top bar
// (title and close button), the filter row, the sessions table, and the Details
// (gauges over text), all in one card.
func (s *Scene) buildPanel() {
	px := s.px
	bar := toolkit.NewHBox()
	bar.Spacing = px(gap / 2)
	bar.AddFlex(s.panelTitle, 1)
	bar.AddFixed(s.closeBtn, px(iconW))

	row := func(ws ...toolkit.Widget) *toolkit.HBox {
		h := toolkit.NewHBox()
		h.Spacing = px(gap / 2)
		for _, w := range ws {
			h.AddFlex(w, 1)
		}
		return h
	}
	// The Sessions card is one column: the filter rows above the table. The rows are
	// optional: a pane too short for them to leave the table a readable share
	// drops them (the box does that, not this code), the model check boxes first.
	// The state, activity and repository filters and the Clear button outrank the
	// check boxes; the table keeps the rest.
	sessions := toolkit.NewVBox()
	sessions.Spacing = px(gap / 2)
	sessions.AddOptional(row(s.stateDD, s.lastDD), px(controlsH), px(minTableH))
	clearRow := toolkit.NewHBox()
	clearRow.Spacing = px(gap / 2)
	clearRow.AddFlex(s.repoDD, 1)
	clearRow.AddFixed(s.clearBtn, px(clearW))
	sessions.AddOptional(clearRow, px(controlsH), px(minTableH))
	var on []toolkit.Widget
	for i, c := range s.modelChecks {
		if l := s.vm.Filters.ModelLabels[i].Get(); l != "" {
			c.Label = l
			on = append(on, c)
		}
	}
	for len(on) > 0 {
		n := min(3, len(on))
		sessions.AddOptional(row(on[:n]...), px(controlsH), px(minTableH))
		on = on[n:]
	}
	sessions.AddFlex(s.table, 1)

	details := toolkit.NewVBox()
	details.Spacing = px(gap / 2)
	details.AddFlex(s.ctxTable, 1)
	details.AddFlex(s.detail, 3)

	// Sessions over Details: the divider between them is the one the owner drags.
	// Its panes hold at least PaneMinH each, and the ratio is the ViewModel's Split.
	side := toolkit.NewVBox()
	side.Spacing = px(gap / 2)
	side.AddFixed(bar, px(controlsH))
	side.AddFlex(s.stack(card(T("card.sessions"), sessions, cardPad), card(T("card.details"), details, cardPad)), 1)
	s.panel = card("", side, cardPad)
	s.panelFocus = []toolkit.Widget{s.table, s.detail, s.ctxTable, s.closeBtn, s.stateDD, s.lastDD, s.repoDD, s.clearBtn}
	for _, c := range s.modelChecks {
		s.panelFocus = append(s.panelFocus, c)
	}
}

// minTableH is the least height, in logical pixels, the sessions table keeps.
const minTableH = 100

// clearW and renameW are the widths of the "Clear filters" and "Rename" buttons.
const (
	clearW  = 110
	renameW = 110
)

// mirror keeps two observables equal, whichever is set; a value already equal is
// not written back, so the pair does not echo.
func mirror[T comparable](a, b *mvvm.Observable[T]) []func() {
	return []func(){
		a.SubscribeChanged(func() {
			if b.Get() != a.Get() {
				b.Set(a.Get())
			}
		}),
		b.SubscribeChanged(func() {
			if a.Get() != b.Get() {
				a.Set(b.Get())
			}
		}),
	}
}

// GraphMinW is the least width, in logical pixels, the graph keeps beside the
// open panel: the splitter squeezes the panel before it takes more.
const GraphMinW = 320

// dock puts the graph and the panel side by side in a splitter: the graph takes
// what the panel leaves, the panel's width is the ViewModel's PanelW, and its left
// edge is the divider the owner drags, resizing both live. The panel pane is
// shown exactly while PanelOpen: opening it shrinks the graph (which lays itself
// out again in the narrower rectangle), closing it gives the width back, and the
// width is remembered in PanelW. The panel is at least PanelMinW wide and at most
// PanelMaxPercent of the surface, the graph at least GraphMinW; the splitter
// enforces them while dragging, and the ViewModel clamps what a store restores.
func (s *Scene) dock() {
	hi := max(int(float64(s.w)/s.scale)*PanelMaxPercent/100, PanelMinW)
	// The splitter holds on to the observables it is given for as long as they
	// live, and every layout makes a new splitter: it is given its own copies,
	// mirrored two ways with the ViewModel's, and the mirrors of the splitter that
	// is replaced are dropped, so it never lays out a widget that is no longer
	// its own.
	for _, u := range s.dockUnsub {
		u()
	}
	s.dockUnsub = nil
	size, shown := mvvm.NewObservable(s.vm.PanelW.Get()), mvvm.NewObservable(s.vm.PanelOpen.Get())
	s.dockUnsub = append(s.dockUnsub, mirror(s.vm.PanelW, size)...)
	s.dockUnsub = append(s.dockUnsub, mirror(s.vm.PanelOpen, shown)...)
	s.panelPane = &toolkit.SplitPane{Widget: s.panel, Name: "side panel", Size: size, Min: PanelMinW, Max: hi, Visible: shown}
	s.split = toolkit.NewSplitter(false,
		&toolkit.SplitPane{Widget: card("", s.graph, cardPad/2), Name: "graph", Weight: mvvm.NewObservable(1), Min: GraphMinW}, s.panelPane)
	s.split.Now = func() time.Time { return s.clock() } // its double-click detection reads the scene's clock
}

// tokens2 is the row of the output and cache-read token charts.
func tokens2(s *Scene, px func(int) int) toolkit.Widget {
	h := toolkit.NewHBox()
	h.Spacing = px(gap)
	h.AddFlex(card(T("card.output"), s.outBars, cardPad), 1)
	h.AddFlex(card(T("card.cache"), s.cacheBars, cardPad), 1)
	return h
}

// quotaPage is the Quota tab: the span and kind selectors over the charts, and
// the notes under them. The "range" layout serves the 5 h and 7 day spans, the
// "weeks" layout the 4 x 7 days one.
func (s *Scene) quotaPage() toolkit.Widget {
	px := s.px
	// Two rows, so the selectors fit a narrow window: the account and the span,
	// then the token kind of the 4 x 7 days span.
	first := toolkit.NewHBox()
	first.Spacing = px(gap / 2)
	first.AddFixed(toolkit.NewLabel(T("lbl.account")), px(64))
	first.AddFlex(s.accountDD, 1)
	first.AddFixed(toolkit.NewLabel(""), px(gap))
	first.AddFixed(toolkit.NewLabel(T("lbl.span")), px(48))
	for _, r := range s.spanRadios {
		first.AddFixed(r, px(radioW))
	}
	second := toolkit.NewHBox()
	second.Spacing = px(gap / 2)
	second.AddFixed(toolkit.NewLabel(T("quota.tokens")), px(150))
	for _, r := range s.kindRadios {
		second.AddFixed(r, px(radioW))
	}
	second.AddFlex(toolkit.NewLabel(""), 1)
	// Rename acts on the account selected in the table below.
	second.AddFixed(s.renameBtn, px(renameW))
	second.AddFixed(s.colorBtn, px(renameW))
	controls := toolkit.NewVBox()
	controls.Spacing = px(gap / 4)
	controls.AddFixed(first, px(controlsH))
	controls.AddFixed(second, px(controlsH))

	tokens := toolkit.NewHBox()
	tokens.Spacing = px(gap)
	tokens.AddFlex(card(T("card.output"), s.outBars, cardPad), 1)
	tokens.AddFlex(card(T("card.cache"), s.cacheBars, cardPad), 1)
	rng := toolkit.NewVBox()
	rng.Spacing = px(gap)
	rng.AddFlex(card(T("card.official"), s.official, cardPad), 3)
	rng.AddFlex(tokens, 2)

	upper := toolkit.NewHBox()
	upper.Spacing = px(gap)
	upper.AddFlex(card(T("card.weekly"), s.weekBars, cardPad), 1)
	upper.AddFlex(card(T("card.cumulative"), s.cum, cardPad), 1)
	wk := toolkit.NewVBox()
	wk.Spacing = px(gap)
	wk.AddFlex(upper, 3)
	wk.AddFlex(card(T("card.weekpct"), s.weekPct, cardPad), 2)

	// The aggregated layouts add the capacity table (every account's 5 h and
	// weekly percentages side by side) under the charts.
	capCard := func() *toolkit.Frame { return card(T("card.accounts"), s.capTable, cardPad) }
	allRange := toolkit.NewVBox()
	allRange.Spacing = px(gap)
	allRange.AddFlex(card(T("card.officialby"), s.official, cardPad), 5)
	allRange.AddFlex(tokens2(s, px), 4)
	allRange.AddFlex(capCard(), 3)
	allUpper := toolkit.NewHBox()
	allUpper.Spacing = px(gap)
	allUpper.AddFlex(card(T("card.perday"), s.weekBars, cardPad), 1)
	allUpper.AddFlex(card(T("card.weekpctby"), s.weekPct, cardPad), 1)
	allWeeks := toolkit.NewVBox()
	allWeeks.Spacing = px(gap)
	allWeeks.AddFlex(allUpper, 5)
	allWeeks.AddFlex(capCard(), 3)

	s.quota = toolkit.NewStack()
	s.quota.AddPage("range", rng)
	s.quota.AddPage("weeks", wk)
	s.quota.AddPage("allrange", allRange)
	s.quota.AddPage("allweeks", allWeeks)

	page := toolkit.NewVBox()
	page.Spacing = px(gap)
	page.AddFixed(controls, 2*px(controlsH)+px(gap/4))
	page.AddFlex(s.quota, 1)
	page.AddFixed(s.notes, px(toolkit.StatusbarH))
	return page
}

// syncPages shows the page of the tab, and the layout of the span.
func (s *Scene) syncPages() {
	pages := []string{"graph", "quota", "guardrails"}
	s.pages.SetVisible(pages[min(max(s.vm.Tab.Get(), 0), len(pages)-1)])
	page := "range"
	if s.vm.Span.Get() == SpanWeeks {
		page = "weeks"
	}
	if s.vm.Account.Get() == 0 {
		page = "all" + page
	}
	s.quota.SetVisible(page)
	// The page that was hidden has no bounds yet.
	s.pages.SetBounds(s.pages.Bounds())
}

// PanelVisible is whether the side panel is on screen: open, over the graph.
func (s *Scene) PanelVisible() bool {
	return s.vm.PanelOpen.Get() && s.vm.Tab.Get() == TabGraph
}

// openDialog is the modal dialog that is up, nil when there is none, and its size
// in logical pixels.
func (s *Scene) openDialog() (d *toolkit.Dialog, w, h int) {
	switch {
	case s.vm.Rename.Open.Get():
		return s.renameDlg, dialogW, dialogH
	case s.vm.Color.Open.Get():
		return s.colorDlg, dialogW, toolkit.ColorPickerHeight + colorDialogExtra
	}
	return nil, 0, 0
}

// syncPanel puts the panel (in its splitter) and, above it, the open dialog in
// the overlay, or takes them out.
func (s *Scene) syncPanel() {
	s.overlay.Clear()
	if d, lw, lh := s.openDialog(); d != nil {
		// The toolkit's ModalWindow places the dialog: centred in the surface and
		// clamped to fit, its size in logical pixels. The scrim is its own; a click on
		// it does not dismiss, the dialog closes by its buttons and Escape.
		m := toolkit.NewModalWindow("", nil)
		m.Panel, m.CloseOnScrim, m.PanelW, m.PanelH = d, false, lw, lh
		m.SetBounds(toolkit.Rect{W: s.w, H: s.h})
		s.overlay.Push(m)
	}
}

// syncDialog shows or hides the dialogs, with the Rename entry focused.
func (s *Scene) syncDialog() {
	s.syncPanel()
	open := s.vm.Rename.Open.Get()
	s.renameEntry.SetFocused(open)
	if open {
		s.keyTarget = s.renameEntry
	} else if s.keyTarget == toolkit.Widget(s.renameEntry) {
		s.keyTarget = nil
	}
}

// The Rename dialog's size, in logical pixels.
const (
	dialogW = 400
	dialogH = 190
	// colorDialogExtra is what the Colour dialog holds besides the picker: its label,
	// error line, the gaps, the title and the buttons.
	colorDialogExtra = 22 + 6 + 6 + 22 + 60
)

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
	s.host.Draw(p, s.theme)
}

// DrawTo paints the scene, ground included, through any painter: the same widget
// tree Draw paints, so a recording painter (the demo capture) shows exactly what
// the live screen does.
func (s *Scene) DrawTo(p painter.Painter) {
	p.FillRect(painter.Rect{W: s.w, H: s.h}, s.theme.Background)
	s.host.Draw(p, s.theme)
}

// targets are the widgets that take a click, topmost first: the panel's over the
// graph's, then the header's, then those of the shown tab.
func (s *Scene) targets() []toolkit.Widget {
	var t []toolkit.Widget
	if d, _, _ := s.openDialog(); d != nil { // the dialog is modal: it takes every click
		return []toolkit.Widget{d}
	}
	if s.PanelVisible() {
		// A row a short Sessions pane has no room for is laid out empty, and the
		// router gives an empty rectangle no click.
		t = append(t, s.closeBtn, s.clearBtn, s.stateDD, s.lastDD, s.repoDD)
		for _, c := range s.modelChecks {
			t = append(t, c)
		}
		t = append(t, s.table, s.ctxTable, s.detail)
	}
	t = append(t, s.search, s.expired, s.themeDD, s.langDD, s.toggle, s.tabs)
	switch s.vm.Tab.Get() {
	case TabGraph:
		return append(t, s.graph)
	case TabGuardrails:
		return append(t, s.gDecision, s.gTable, s.gDetail)
	}
	t = append(t, s.accountDD, s.capTable, s.renameBtn, s.colorBtn)
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
	for _, dd := range s.dropDowns() {
		if dd.PopoverOpen() { // the open list takes the click (a click outside closes it)
			dd.PopoverClick(x, y)
			s.dirty = true
			return true
		}
	}
	if s.dialogOpen() {
		return s.clickDialog(x, y)
	}
	// The router gives the press to a divider (the panel's left edge, the one
	// between Sessions and Details) or to the widget under the pointer, and holds
	// it until the release.
	s.ptr.Press(x, y, "")
	return true
}

// dropDowns are the drop-downs, whose open lists take the next click.
func (s *Scene) dropDowns() []*toolkit.DropDown {
	return []*toolkit.DropDown{s.themeDD, s.langDD, s.accountDD, s.gDecision, s.stateDD, s.lastDD, s.repoDD}
}

// clickDialog gives a click to the open dialog; a click outside it is
// swallowed, the dialog being modal. The Rename dialog takes the click, not a
// drag; the Colour dialog keeps the grab, so a drag in its picker goes on.
func (s *Scene) clickDialog(x, y int) bool {
	if s.ptr.Press(x, y, "") != nil {
		s.renameEntry.SetFocused(s.vm.Rename.Open.Get())
	}
	if s.vm.Rename.Open.Get() {
		s.ptr.Cancel()
	}
	return true
}

// HandleScroll routes a wheel scroll (Delta in rows, positive = down) to the
// scrollable under the pointer.
func (s *Scene) HandleScroll(x, y, delta int) bool {
	s.ptr.Scroll(x, y, delta)
	return true
}

// HandleChar routes a printable character to the focused widget; "t" outside the
// search box toggles the side panel.
func (s *Scene) HandleChar(ch string) bool {
	if s.vm.Rename.Open.Get() { // the dialog's entry takes every character
		s.renameDlg.OnEvent(toolkit.Event{Kind: toolkit.EventChar, Code: ch})
		return true
	}
	if s.vm.Color.Open.Get() { // the picker takes none: nothing behind the dialog hears it
		return true
	}
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
	if s.vm.Rename.Open.Get() { // Enter confirms the dialog, Escape cancels it, the rest is typing
		switch code {
		case "Escape":
			s.vm.Rename.Cancel.Execute()
		case "Enter":
			s.vm.Rename.Confirm.Execute()
		default:
			s.renameDlg.OnEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: code})
		}
		return true
	}
	if s.vm.Color.Open.Get() { // Enter confirms the dialog, Escape cancels it, the rest is ignored
		switch code {
		case "Escape":
			s.vm.Color.Cancel.Execute()
		case "Enter":
			s.vm.Color.Confirm.Execute()
		}
		return true
	}
	if code == "Escape" {
		// With the focus in the side panel, Escape closes it: the command the t key
		// and the close button run. Anywhere else it folds every open org (the graph
		// also clears its selection).
		if s.PanelVisible() && s.inPanel(s.keyTarget) {
			s.vm.TogglePanel.Execute()
			return true
		}
		s.vm.CollapseAll()
	}
	if s.keyTarget == nil {
		return code == "Escape"
	}
	s.keyTarget.OnEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: code})
	return true
}

// inPanel reports whether w is one of the side panel's widgets, or its divider:
// what "the panel has the focus" means.
func (s *Scene) inPanel(w toolkit.Widget) bool {
	// A nil w equals no widget below, so it needs no refusal of its own.
	if w == toolkit.Widget(s.split) || w == toolkit.Widget(s.vsplit) {
		return true
	}
	for _, p := range s.panelFocus {
		if w == p {
			return true
		}
	}
	return false
}

// HandleMove routes pointer motion to the widget grabbed on press; with nothing
// grabbed it tracks the pointer over the panel's divider (which lights up). It
// reports whether the picture changed.
func (s *Scene) HandleMove(x, y int) bool {
	if s.ptr.Drag(x, y) {
		return true
	}
	return s.hoverDivider(x, y)
}

// hoverDivider tells the splitter where the pointer is, and reports whether that
// changed which divider is under it.
func (s *Scene) hoverDivider(x, y int) bool {
	if !s.PanelVisible() {
		old, oldV := s.hover, s.vhover
		s.hover, s.vhover = -1, -1
		return old != -1 || oldV != -1
	}
	s.ptr.Hover(x, y)
	h, v := s.split.Hovered(), s.vsplit.Hovered()
	changed := h != s.hover || v != s.vhover
	s.hover, s.vhover = h, v
	return changed
}

// Cursor is the CSS cursor the pointer at surface (x, y) should show: a
// horizontal resize over the panel's divider and while it is held, "" (the
// host's default) elsewhere.
func (s *Scene) Cursor(x, y int) string {
	if !s.PanelVisible() {
		return ""
	}
	return s.ptr.Cursor(x, y) // a vertical resize over the Sessions/Details divider, a horizontal one over the panel's
}

// HandleRelease routes a button release to the grabbed widget and drops it.
func (s *Scene) HandleRelease(x, y int) bool {
	return s.ptr.Release(x, y)
}

func fillBG(buf []byte, c toolkit.RGBA) {
	for i := 0; i+3 < len(buf); i += 4 {
		buf[i], buf[i+1], buf[i+2], buf[i+3] = c.R, c.G, c.B, c.A
	}
}
