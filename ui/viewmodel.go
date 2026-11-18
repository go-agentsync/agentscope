// Package ui is the agentscope screen: a ViewModel that holds ALL state as
// go-widgets/mvvm primitives and never references a widget, and a View
// (scene.go) that builds toolkit widgets and binds them to it. Both are
// tagless, so a native `go test` covers them; the browser host is
// cmd/agentscope-web.
package ui

import (
	"strconv"
	"strings"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/mvvm"
	"github.com/go-widgets/toolkit"
)

// Row is one line of the sessions table.
type Row struct {
	ID    string // session short id
	State string
	Repo  string
	Task  string
	Last  string
	Agent string // sub-agent count
}

// Cell widths, in characters, that fit the table's columns: the Table paints
// a cell's text without clipping it, so the text is cut to fit.
const (
	repoChars = 22
	taskChars = 38
)

// Cells is the row as the table shows it.
func (r Row) Cells() []string {
	return []string{r.State, r.ID, model.Truncate(r.Repo, repoChars), model.Truncate(r.Task, taskChars), r.Last, r.Agent}
}

// Text is the row's whole text, which is what the query is matched against.
func (r Row) Text() string {
	return strings.Join([]string{r.State, r.ID, r.Repo, r.Task, r.Last, r.Agent}, " ")
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

	snap     model.Snapshot
	have     bool
	allRows  []Row
	notDrawn string
	gd       graphData // the graph as built from the last snapshot
}

// NewViewModel returns a ViewModel showing nothing yet.
func NewViewModel() *ViewModel {
	vm := &ViewModel{
		Graph:    toolkit.NewGraphViewModel(),
		Query:    mvvm.NewObservable(""),
		Rows:     mvvm.NewObservableList[Row](),
		TableSel: mvvm.NewObservable(-1),
		Detail:   mvvm.NewObservableList[string](),
		Status:   mvvm.NewObservable("waiting for the collector..."),
		Conn:     mvvm.NewObservable("connecting"),
	}
	vm.Query.SubscribeChanged(vm.onQuery)
	vm.Graph.Selected.SubscribeChanged(vm.onGraphSelected)
	vm.TableSel.SubscribeChanged(vm.onTableSelected)
	return vm
}

// Snapshot is the last snapshot applied.
func (vm *ViewModel) Snapshot() (model.Snapshot, bool) { return vm.snap, vm.have }

// Apply shows a snapshot. A list is replaced only when it differs, so a graph
// whose members did not change does not lay itself out again.
func (vm *ViewModel) Apply(s model.Snapshot) {
	vm.snap, vm.have = s, true
	g := buildGraph(s)
	vm.gd = g
	setList(vm.Graph.Clusters, g.clusters)
	setList(vm.Graph.Nodes, g.nodes)
	vm.refreshEdges()
	vm.notDrawn = ""
	if g.sessionsNotDrawn > 0 || g.repoNotDrawn > 0 {
		vm.notDrawn = " | not drawn: " + strconv.Itoa(g.sessionsNotDrawn) + " sessions, " + strconv.Itoa(g.repoNotDrawn) + " repos (see the table)"
	}

	vm.allRows = vm.allRows[:0]
	for _, se := range s.Sessions {
		vm.allRows = append(vm.allRows, vm.row(s, se))
	}
	vm.refreshRows()
	vm.refreshDetail()

	c := s.Counts()
	vm.Status.Set(strconv.Itoa(c.Sessions) + " sessions (" + strconv.Itoa(c.Active) + " active in " + s.ActiveFor + ") | " +
		strconv.Itoa(c.Agents) + " sub-agents | " + strconv.Itoa(c.Repos) + " repos (" + strconv.Itoa(c.Held) + " held) | " +
		strconv.Itoa(c.Tells) + " tells | " + strconv.Itoa(c.Anomalies) + " anomalies | " + s.At.Format("15:04:05") + vm.notDrawn)
}

// row builds a session's table row.
func (vm *ViewModel) row(s model.Snapshot, se model.Session) Row {
	state := "idle"
	switch {
	case !se.HasTranscript:
		state = "no tr."
	case se.Active:
		state = "ACTIVE"
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
	return Row{ID: se.Short, State: state, Repo: repo, Task: s.Task(se), Last: ago(s.At, se.LastActivity), Agent: strconv.Itoa(len(se.Agents))}
}

// refreshRows applies the query to the full row set.
func (vm *ViewModel) refreshRows() {
	q := strings.ToLower(strings.TrimSpace(vm.Query.Get()))
	var rows []Row
	for _, r := range vm.allRows {
		if q == "" || strings.Contains(strings.ToLower(r.Text()), q) {
			rows = append(rows, r)
		}
	}
	setList(vm.Rows, rows)
	vm.syncTableSel()
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
	vm.refreshEdges()
	vm.syncTableSel()
	vm.refreshDetail()
}

// refreshEdges sets the edge list for the current selection. Only edges
// change, so the graph does not move.
func (vm *ViewModel) refreshEdges() {
	setList(vm.Graph.Edges, vm.gd.edges(vm.Graph.Selected.Get()))
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
