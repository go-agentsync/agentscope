// Package ui is the agentscope screen: a ViewModel that holds ALL state as
// go-widgets/mvvm primitives and never references a widget, and a View
// (scene.go) that builds toolkit widgets and binds them to it. Both are
// tagless, so a native `go test` covers them; the browser host is
// cmd/agentscope-web.
package ui

import (
	"crypto/sha256"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/mvvm"
	"github.com/go-widgets/toolkit"
)

// The tabs.
const (
	TabGraph = iota
	TabQuota
	TabGuardrails
)

// ModelChipSlots is how many model families the header shows chips for.
const ModelChipSlots = 3

// Row is one line of the sessions table.
type Row struct {
	ID    string // session short id
	State string
	Repo  string
	Task  string
	Last  string
	Agent string // sub-agent count
	Model string // short family name ("Opus 5.5"), or "?" when unknown or unseen
	// CtxText is "212k / 300k", Ratio the context over the compaction window, and
	// Band its pressure band (model.PressureBand): what the Context column's
	// gauge draws.
	CtxText string
	Ratio   float64
	Band    int
	// Repos is every repository the session holds or sits in, one per line: what
	// the Repository filter matches (a session can hold several). Active and Age
	// are what the State and Last-activity filters read.
	Repos  string
	Active bool
	Age    time.Duration
}

// Cell widths, in characters, that fit the table's columns: the Table paints
// a cell's text without clipping it, so the text is cut to fit.
const (
	repoChars = 22
	taskChars = 34
)

// modelShort is the table's name for a model id: the family and version, or
// "?" for a missing or unrecognised one (the raw id is in the detail pane).
func modelShort(id string) string {
	name, _ := model.ModelName(id)
	return name
}

// Cells is the row as the table shows it.
func (r Row) Cells() []string {
	return []string{r.State, r.ID, r.CtxText, r.Model, model.Truncate(r.Repo, repoChars), model.Truncate(r.Task, taskChars), r.Last, r.Agent}
}

// Text is the row's whole text, which is what the query is matched against.
func (r Row) Text() string {
	return strings.Join([]string{r.State, r.ID, r.Model, r.Repo, r.Task, r.Last, r.Agent}, " ")
}

// ViewModel is the whole screen's state.
type ViewModel struct {
	// Graph is what the GraphView shows; its Selected and Filter are the
	// widget's own observables.
	Graph *toolkit.GraphViewModel
	// Query is the search entry's text. It dims the graph (Graph.Filter) and
	// narrows the table.
	Query *mvvm.Observable[string]
	// Rows is the sessions table; TableSel is its selected row index.
	Rows     *mvvm.ObservableList[Row]
	TableSel *mvvm.Observable[int]
	// Detail is the detail pane: the selection's facts, or the anomalies when
	// nothing is selected.
	Detail *mvvm.ObservableList[string]
	// Status is the one-line summary; Conn is the connection state.
	Status *mvvm.Observable[string]
	Conn   *mvvm.Observable[string]
	// ShowExpired draws the edges to a selected session's expired leases
	// (hidden by default: the detail pane lists them). Expanded is the orgs of
	// the "other repositories" cluster that are open.
	ShowExpired *mvvm.Observable[bool]
	Expanded    *mvvm.ObservableList[string]
	// Dark is the system's colour-scheme preference, set by the host (a
	// browser's prefers-color-scheme) and followed when it changes.
	Dark *mvvm.Observable[bool]
	// ModelChips are the header's model-count chips: the three most used model
	// families ("Sonnet 29"), "" for a slot with none.
	ModelChips [ModelChipSlots]*mvvm.Observable[string]

	// Guard is the Guardrails tab's state (see guardrails.go).
	Guard *GuardState
	// Tab is the shown tab: TabGraph, TabQuota or TabGuardrails.
	Tab *mvvm.Observable[int]
	// PanelOpen is whether the side panel (the sessions table and the details)
	// is open over the graph; it starts closed, TogglePanel flips it, and
	// selecting a node opens it.
	PanelOpen   *mvvm.Observable[bool]
	TogglePanel *mvvm.Command
	// Span is the Quota tab's span (SpanShort, SpanWeek, SpanWeeks) and Kind the
	// token kind its 4 x 7 days charts show.
	Span, Kind *mvvm.Observable[int]
	// Usage is the Quota tab's data, from the last snapshot.
	Usage *mvvm.Observable[*model.Usage]
	// QuotaNotes is the line under the Quota charts: what is not available, what
	// is not recorded, where the reset came from.
	QuotaNotes *mvvm.Observable[string]
	// Account is the account selector's choice: 0 is AllAccounts, i is the i-th
	// account of the usage. AccountOptions are its options and Capacity the
	// capacity table's rows, both rebuilt when the usage changes; the selection
	// follows its account (by key) across a rebuild.
	Account        *mvvm.Observable[int]
	AccountOptions *mvvm.ObservableList[string]
	Capacity       *mvvm.ObservableList[CapRow]
	selKey         string // the selected account's key, or allKey

	// Filters narrow the sessions table (AND, with the query).
	Filters *Filters
	// Contexts are the Details pane's gauges: the selected session and each of its
	// sub-agents, or the selected sub-agent.
	Contexts *mvvm.ObservableList[CtxRow]
	// ViewW is the surface's width in logical pixels (set by the Scene), PanelW the
	// side panel's, kept between PanelMinW and PanelMaxPercent of it; ColW the
	// sessions table's column widths. All three are persisted per viewer (SetStore).
	ViewW, PanelW *mvvm.Observable[int]
	ColW          *mvvm.ObservableList[int]
	// Split is the Sessions pane's share, in thousandths, of the Sessions and Details
	// panes stacked in the side panel (see vsplit.go); persisted per viewer too.
	Split *mvvm.Observable[int]

	// The Rename action of the accounts table: CapSel is the selected row, and
	// Rename the dialog's state (see rename.go).
	CapSel *mvvm.Observable[int]
	Rename *RenameState
	// Color is the Colour action of the same table: the dialog that gives the
	// selected account a colour of its own (see colorstate.go).
	Color *ColorState
	// Prefs is the viewer's theme and language (see prefs.go): Dark below follows
	// it, and so does every text the screen shows.
	Prefs *PrefsState

	connKey, connDetail string // the connection state SetConn last showed
	usageSig            string // a hash of the Usage last set, "" for none
	snap                model.Snapshot
	have                bool
	allRows             []Row
	notDrawn            string
	gd                  graphData // the graph as built from the last snapshot
}

// NewViewModel returns a ViewModel showing nothing yet.
func NewViewModel() *ViewModel {
	toolkit.SetLocalizer(nil) // a new screen starts in English, whatever language the last one was left in
	vm := &ViewModel{
		Graph:    toolkit.NewGraphViewModel(),
		Query:    mvvm.NewObservable(""),
		Rows:     mvvm.NewObservableList[Row](),
		TableSel: mvvm.NewObservable(-1),
		Detail:   mvvm.NewObservableList[string](),
		Status:   mvvm.NewObservable(T("status.waiting")),
		Conn:     mvvm.NewObservable(T("conn.connecting")),
		connKey:  "connecting",

		ShowExpired: mvvm.NewObservable(false),
		Dark:        mvvm.NewObservable(false),
		Expanded:    mvvm.NewObservableList[string](),

		Guard:          newGuardState(),
		Tab:            mvvm.NewObservable(TabGraph),
		PanelOpen:      mvvm.NewObservable(false),
		Span:           mvvm.NewObservable(SpanWeek),
		Kind:           mvvm.NewObservable(KindOutput),
		Usage:          mvvm.NewObservable[*model.Usage](nil),
		QuotaNotes:     mvvm.NewObservable(""),
		Account:        mvvm.NewObservable(0),
		selKey:         allKey,
		AccountOptions: mvvm.NewObservableList(AllAccounts()),
		Capacity:       mvvm.NewObservableList[CapRow](),

		Contexts: mvvm.NewObservableList[CtxRow](),
		ViewW:    mvvm.NewObservable(DefaultW),
		PanelW:   mvvm.NewObservable(PanelDefaultW),
		ColW:     mvvm.NewObservableList(ColDefault...),
		Split:    mvvm.NewObservable(SplitDefault),
		CapSel:   mvvm.NewObservable(-1),
	}
	vm.Split.SubscribeChanged(func() {
		if v := ClampSplit(vm.Split.Get()); v != vm.Split.Get() {
			vm.Split.Set(v)
		}
	})
	vm.Filters = newFilters(vm.refreshRows)
	vm.Rename = newRenameState(vm)
	vm.Color = newColorState(vm)
	vm.Prefs = newPrefsState(vm)
	// A width the owner (or a store) sets is held to the bounds at once. A surface
	// that gets narrower does NOT rewrite it: the panel is only shown within the
	// bounds (EffectivePanelW), so widening the window brings the chosen width back.
	vm.PanelW.SubscribeChanged(func() {
		if w := ClampPanelW(vm.PanelW.Get(), vm.ViewW.Get()); w != vm.PanelW.Get() {
			vm.PanelW.Set(w)
		}
	})
	for i := range vm.ModelChips {
		vm.ModelChips[i] = mvvm.NewObservable("")
	}
	vm.TogglePanel = mvvm.NewCommand(func() { vm.PanelOpen.Set(!vm.PanelOpen.Get()) }, nil)
	vm.Query.SubscribeChanged(vm.onQuery)
	vm.Usage.SubscribeChanged(vm.onUsage)
	vm.Account.SubscribeChanged(vm.onAccount)
	vm.ShowExpired.SubscribeChanged(vm.refreshEdges)
	vm.Expanded.SubscribeChanged(func() {
		if vm.have {
			vm.rebuildGraph()
			vm.refreshDetail()
		}
	})
	vm.Graph.Selected.SubscribeChanged(vm.onGraphSelected)
	vm.TableSel.SubscribeChanged(vm.onTableSelected)
	return vm
}

// allKey is the selected key when the selector is on AllAccounts (no account's
// key is empty-with-NUL).
const allKey = "\x00all"

// onUsage rebuilds the selector's options and the capacity rows, and keeps the
// selection on the same account: by key, never by position.
func (vm *ViewModel) onUsage() {
	u := vm.Usage.Get()
	setList(vm.AccountOptions, AccountOptions(u))
	var rows []CapRow
	idx := 0
	if u != nil {
		rows = capacityRows(u.Capacity)
		for i, a := range u.Accounts {
			if a.Key == vm.selKey {
				idx = i + 1
			}
		}
	}
	if idx == 0 {
		vm.selKey = allKey
	}
	setList(vm.Capacity, rows)
	if vm.CapSel.Get() >= len(rows) {
		vm.CapSel.Set(-1)
	}
	// Setting the selector runs onAccount, which derives selKey again from the
	// same usage and index: the key just chosen, so it is a no-op and needs no
	// guard against being re-entered.
	vm.Account.Set(idx)
}

// onAccount follows the selector.
func (vm *ViewModel) onAccount() {
	u, i := vm.Usage.Get(), vm.Account.Get()
	if u == nil || i <= 0 || i > len(u.Accounts) {
		vm.selKey = allKey
		return
	}
	vm.selKey = u.Accounts[i-1].Key
}

// Snapshot is the last snapshot applied.
func (vm *ViewModel) Snapshot() (model.Snapshot, bool) { return vm.snap, vm.have }

// Apply shows a snapshot. A list is replaced only when it differs, so a graph
// whose members did not change does not lay itself out again.
func (vm *ViewModel) Apply(s model.Snapshot) {
	vm.snap, vm.have = s, true
	if s.Usage != nil {
		vm.Prefs.Apply(s.Usage.Prefs) // may change the language, which applies the snapshot again
	}
	vm.Guard.Data.Set(s.Guardrails)
	// A new Usage value with the same content is not a change: setting it would
	// rebuild every chart and repaint the Quota tab for nothing.
	if sig := usageSig(s.Usage); sig != vm.usageSig {
		vm.usageSig = sig
		vm.Usage.Set(s.Usage)
	}
	vm.rebuildGraph()

	vm.allRows = vm.allRows[:0]
	for _, se := range s.Sessions {
		vm.allRows = append(vm.allRows, vm.row(s, se))
	}
	vm.Filters.sync(vm.allRows)
	vm.refreshRows()
	vm.refreshDetail()

	counts := s.ModelCounts()
	for i, chip := range vm.ModelChips {
		text := ""
		if i < len(counts) {
			text = counts[i].Family + " " + strconv.Itoa(counts[i].N)
		}
		chip.Set(text)
	}
	vm.refreshStatus()
}

// SetConn shows the connection state: key is one of "connecting", "connected",
// "dial", "lost" or "ended" (the catalog's conn.<key>), detail what went wrong, if
// anything. It is kept, so a change of language says it again in the new one.
func (vm *ViewModel) SetConn(key, detail string) {
	vm.connKey, vm.connDetail = key, detail
	text := T("conn." + key)
	if detail != "" {
		text += ": " + detail
	}
	vm.Conn.Set(text)
}

// relocalize rebuilds every text the ViewModel holds, in the language that has just
// been put in force: the account options, the repository options, and (with a
// snapshot) the rows, the chips, the details and the status line.
func (vm *ViewModel) relocalize() {
	vm.SetConn(vm.connKey, vm.connDetail)
	setList(vm.Prefs.ThemeOpts, ThemeNames())
	setList(vm.Prefs.LangOpts, LangNames())
	setList(vm.Filters.StateOpts, StateNames())
	setList(vm.Filters.LastOpts, LastNames())
	setList(vm.Guard.DecisionOpts, DecisionNames())
	vm.Guard.onData() // the chips, the notes and the rows are words too
	vm.Guard.refreshDetail()
	if vm.have {
		vm.Apply(vm.snap)
		vm.onUsage() // the same usage is not a change to Apply, but its notes have new words
		return
	}
	vm.Status.Set(T("status.waiting"))
	vm.onUsage()
	vm.Filters.sync(nil)
}

// rebuildGraph maps the last snapshot onto the graph, with the orgs that are
// expanded now. Clusters and nodes are replaced only when they differ.
func (vm *ViewModel) rebuildGraph() {
	exp := map[string]bool{}
	for _, o := range vm.Expanded.Slice() {
		exp[o] = true
	}
	g := buildGraph(vm.snap, exp)
	vm.gd = g
	setList(vm.Graph.Clusters, g.clusters)
	setList(vm.Graph.Nodes, g.nodes)
	vm.refreshEdges()
	vm.notDrawn = ""
	if g.sessionsNotDrawn > 0 || g.repoNotDrawn > 0 {
		vm.notDrawn = Tf("vm.notDrawn", g.sessionsNotDrawn, g.repoNotDrawn)
	}
}

// ToggleOrg expands or collapses an org of the "other repositories" cluster:
// what activating (Enter, a second click on) its node does. selection is the
// graph's Selected value; anything that is not an org node is ignored.
func (vm *ViewModel) ToggleOrg(selection string) {
	org, ok := strings.CutPrefix(selection, "node:"+orgPrefix)
	if !ok {
		return
	}
	cur := vm.Expanded.Slice()
	for i, o := range cur {
		if o == org {
			vm.Expanded.RemoveAt(i)
			return
		}
	}
	vm.Expanded.Append(org)
}

// CollapseAll folds every org again (Escape).
func (vm *ViewModel) CollapseAll() { vm.Expanded.Clear() }

// row builds a session's table row.
func (vm *ViewModel) row(s model.Snapshot, se model.Session) Row {
	state := T("row.idle")
	switch {
	case !se.HasTranscript:
		state = T("row.notr")
	case se.Active:
		state = T("row.active")
	}
	var repos []string
	for _, l := range s.LeasesOf(se.ID) {
		if !l.Expired {
			repos = append(repos, l.Repo)
		}
	}
	repo := strings.Join(repos, ",")
	if repo == "" {
		repo = se.CwdRepo
	}
	// Every repository of the session, for the filter: the ones it holds and the
	// one it sits in.
	all := append([]string(nil), repos...)
	if se.CwdRepo != "" {
		all = append(all, se.CwdRepo)
	}
	age := neverActive
	if !se.LastActivity.IsZero() {
		// To the minute, like the "Last" column: a row that differs in seconds only
		// is not a change, and would repaint the table on every snapshot.
		age = s.At.Sub(se.LastActivity).Truncate(time.Minute)
	}
	c := ctxOf("", se.Context, windowOf(s))
	return Row{ID: se.Short, State: state, Repo: repo, Task: s.Task(se), Last: ago(s.At, se.LastActivity), Agent: strconv.Itoa(len(se.Agents)), Model: modelShort(se.Model),
		CtxText: c.Text, Ratio: c.Ratio, Band: c.Band, Repos: strings.Join(uniq(all), "\n"), Active: se.HasTranscript && se.Active, Age: age}
}

// neverActive is the age of a session with no recorded activity: older than any
// recency filter.
const neverActive = 1000 * 24 * time.Hour

// windowOf is the snapshot's compaction window, or the default when the
// collector sent none.
func windowOf(s model.Snapshot) int64 {
	if s.ContextWindow > 0 {
		return s.ContextWindow
	}
	return model.DefaultCompactWindow
}

// uniq drops repeats, keeping the order.
func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// refreshRows applies the query to the full row set.
func (vm *ViewModel) refreshRows() {
	q := strings.ToLower(strings.TrimSpace(vm.Query.Get()))
	var rows []Row
	for _, r := range vm.allRows {
		if (q == "" || strings.Contains(strings.ToLower(r.Text()), q)) && vm.Filters.keep(r) {
			rows = append(rows, r)
		}
	}
	setList(vm.Rows, rows)
	vm.syncTableSel()
	vm.refreshStatus()
}

// refreshStatus writes the one-line summary: how many sessions the filters leave
// out of how many there are, then the rest of the counts.
func (vm *ViewModel) refreshStatus() {
	if !vm.have {
		return
	}
	s := vm.snap
	c := s.Counts()
	vm.Status.Set(Tf("vm.status", vm.Rows.Len(), len(vm.allRows), c.Active, s.ActiveFor,
		c.Agents, c.Repos, c.Held, c.Tells, c.Anomalies) + vm.notDrawn)
}

// usageSig is a hash of what the Quota tab is built from: equal for two Usage
// values with the same content, "" for none.
func usageSig(u *model.Usage) string {
	if u == nil {
		return ""
	}
	c := *u
	c.At = 0 // the instant the view was built moves every pass and is drawn nowhere
	b, err := json.Marshal(&c)
	if err != nil { // cannot happen for this type; never equal to a real hash
		return "unhashable"
	}
	h := sha256.Sum256(b)
	return string(h[:])
}

// onQuery narrows the table and dims the graph.
func (vm *ViewModel) onQuery() {
	vm.Graph.Filter.Set(strings.TrimSpace(vm.Query.Get()))
	vm.refreshRows()
}

// syncTableSel points the table's selection at the selected session's row, or
// at nothing when it is not listed.
func (vm *ViewModel) syncTableSel() {
	idx := -1
	if short, ok := strings.CutPrefix(vm.Graph.Selected.Get(), "node:"+sessionPrefix); ok {
		for i, r := range vm.Rows.Slice() {
			if r.ID == short {
				idx = i
			}
		}
	}
	vm.TableSel.Set(idx)
}

// onGraphSelected follows a click in the graph: the edges (focus), the table
// and the detail pane.
func (vm *ViewModel) onGraphSelected() {
	// A selection does not open the panel: the panel is docked, so opening it
	// would shrink the graph and move the very node that was just clicked. The
	// details of the selection are in the panel when the owner opens it (t).
	vm.refreshEdges()
	vm.syncTableSel()
	vm.refreshDetail()
}

// refreshEdges sets the edge list for the current selection. Only edges
// change, so the graph does not move.
func (vm *ViewModel) refreshEdges() {
	setList(vm.Graph.Edges, vm.gd.edges(vm.Graph.Selected.Get(), vm.ShowExpired.Get()))
}

// onTableSelected follows a click in the table: it selects the session's node.
func (vm *ViewModel) onTableSelected() {
	i := vm.TableSel.Get()
	if i < 0 || i >= vm.Rows.Len() {
		return
	}
	vm.Graph.Selected.Set("node:" + sessionPrefix + vm.Rows.At(i).ID)
}

// setList replaces the contents of l with items when they differ.
func setList[T comparable](l *mvvm.ObservableList[T], items []T) {
	cur := l.Slice()
	if len(cur) == len(items) {
		same := true
		for i := range cur {
			if cur[i] != items[i] {
				same = false
				break
			}
		}
		if same {
			return
		}
	}
	l.Clear()
	l.Append(items...)
}
