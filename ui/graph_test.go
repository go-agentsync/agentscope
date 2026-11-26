package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/toolkit"
)

func edgeMap(es []toolkit.GraphEdge) map[string]toolkit.GraphEdge {
	m := map[string]toolkit.GraphEdge{}
	for _, e := range es {
		m[e.ID] = e
	}
	return m
}

func TestBuildGraphStructure(t *testing.T) {
	g := buildGraph(snap(), nil)
	clusters := map[string]string{}
	for _, c := range g.clusters {
		clusters[c.ID] = c.Label
	}
	// A held repo names the cluster; a cwd-derived one is marked "(cwd)".
	if clusters["aaaaaaaa"] != "aaaaaaaa o/a" || clusters["bbbbbbbb"] != "bbbbbbbb o/b (cwd)" || clusters["cccccccc"] != "cccccccc" || clusters[reposCluster] != repoClusterLabel() {
		t.Errorf("clusters %v", clusters)
	}
	type nd struct{ cluster, cat, label string }
	got := map[string]nd{}
	for _, n := range g.nodes {
		got[n.ID] = nd{n.Cluster, n.Category, n.Label}
	}
	for id, want := range map[string]nd{
		"s:aaaaaaaa":     {"aaaaaaaa", "session", "aaaaaaaa"},
		"s:bbbbbbbb":     {"bbbbbbbb", "idle session", "bbbbbbbb"},
		"s:cccccccc":     {"cccccccc", "no transcript", "cccccccc"},
		"a:aaaaaaaa:ag1": {"aaaaaaaa", "general-purpose", "fix the build"}, // labelled by its task, coloured by its type
		"a:aaaaaaaa:ag2": {"aaaaaaaa", "agent", "idle one"},
		"r:o/a":          {"aaaaaaaa", "held repo", "o/a"}, // held: inside its holder's cluster
		"r:o/old":        {reposCluster, "repo", "o/old"},  // only an expired lease: in the other cluster
		"r:ghost/x":      {reposCluster, "repo", "ghost/x"},
	} {
		if got[id] != want {
			t.Errorf("node %s = %+v, want %+v", id, got[id], want)
		}
	}
	if g.nodeCluster["r:o/a"] != "aaaaaaaa" || !g.active["aaaaaaaa"] || g.active["bbbbbbbb"] {
		t.Errorf("%v %v", g.nodeCluster, g.active)
	}
}

func TestSubAgentWithoutDescriptionIsLabelledByItsType(t *testing.T) {
	s := snap()
	s.Sessions[0].Agents = []model.Agent{{ID: "q", Type: "Explore"}, {ID: "z"}}
	g := buildGraph(s, nil)
	labels := map[string]string{}
	for _, n := range g.nodes {
		labels[n.ID] = n.Label
	}
	if labels["a:aaaaaaaa:q"] != "Explore" || labels["a:aaaaaaaa:z"] != "agent" {
		t.Errorf("%v", labels)
	}
	long := strings.Repeat("word ", 20)
	s.Sessions[0].Agents = []model.Agent{{ID: "l", Type: "t", Description: long}}
	for _, n := range buildGraph(s, nil).nodes {
		if n.ID == "a:aaaaaaaa:l" && len([]rune(n.Label)) > NodeChars {
			t.Errorf("label of %d runes: %q", len([]rune(n.Label)), n.Label)
		}
	}
}

func TestFocusModeEdges(t *testing.T) {
	g := buildGraph(snap(), nil)

	// Nothing selected: only the edges of ACTIVE sessions, dimmed; tells in full.
	e := edgeMap(g.edges("", false))
	for _, id := range []string{"sp:a:aaaaaaaa:ag1", "sp:a:aaaaaaaa:ag2", "ls:aaaaaaaa:o/a"} {
		if e[id].State != toolkit.GraphEdgeIdle {
			t.Errorf("%s with nothing selected: %+v", id, e[id])
		}
	}
	if _, ok := e["ls:aaaaaaaa:o/old"]; ok {
		t.Error("an expired lease's edge is drawn with nothing selected")
	}
	if e["tl:0"].State != toolkit.GraphEdgeNormal || e["tl:0"].To != "s:bbbbbbbb" || e["tl:1"].To != "r:o/a" || e["tl:1"].From != "s:bbbbbbbb" {
		t.Errorf("tells: %+v %+v", e["tl:0"], e["tl:1"])
	}
	for _, id := range []string{"tl:2", "tl:3", "ls:dddddddd:ghost/x"} {
		if _, ok := e[id]; ok {
			t.Errorf("%s: an edge to or from something that is not drawn", id)
		}
	}
	n := 0
	for id := range e {
		if strings.HasPrefix(id, "ls:aaaaaaaa:o/a") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("a repo held twice by one session has %d lease edges", n)
	}

	// A session selected: its whole cluster is in focus; its EXPIRED leases are
	// history and are not drawn, unless the toggle says so.
	e = edgeMap(g.edges("node:s:aaaaaaaa", false))
	for _, id := range []string{"sp:a:aaaaaaaa:ag1", "sp:a:aaaaaaaa:ag2", "ls:aaaaaaaa:o/a"} {
		if e[id].State != toolkit.GraphEdgeNormal {
			t.Errorf("%s with its session selected: %+v", id, e[id])
		}
	}
	if _, ok := e["ls:aaaaaaaa:o/old"]; ok {
		t.Error("an expired lease's edge drawn with the toggle off")
	}
	e = edgeMap(g.edges("node:s:aaaaaaaa", true))
	if e["ls:aaaaaaaa:o/old"].State != toolkit.GraphEdgeNormal {
		t.Errorf("an expired lease's edge with the toggle on: %+v", e["ls:aaaaaaaa:o/old"])
	}
	e = edgeMap(g.edges("", true))
	if _, ok := e["ls:aaaaaaaa:o/old"]; ok {
		t.Error("the toggle draws expired edges with nothing selected")
	}
	// A repository selected: its edge in full, the rest dimmed.
	e = edgeMap(g.edges("node:r:o/a", false))
	if e["ls:aaaaaaaa:o/a"].State != toolkit.GraphEdgeNormal || e["sp:a:aaaaaaaa:ag1"].State != toolkit.GraphEdgeIdle {
		t.Errorf("repo selected: %+v %+v", e["ls:aaaaaaaa:o/a"], e["sp:a:aaaaaaaa:ag1"])
	}
	// A sub-agent selected.
	e = edgeMap(g.edges("node:a:aaaaaaaa:ag1", false))
	if e["sp:a:aaaaaaaa:ag1"].State != toolkit.GraphEdgeNormal || e["sp:a:aaaaaaaa:ag2"].State != toolkit.GraphEdgeIdle {
		t.Errorf("agent selected: %+v", e)
	}
	// An edge selected is drawn in full even when nothing would draw it.
	e = edgeMap(g.edges("edge:ls:aaaaaaaa:o/old", false))
	if e["ls:aaaaaaaa:o/old"].State != toolkit.GraphEdgeNormal || e["ls:aaaaaaaa:o/a"].State != toolkit.GraphEdgeIdle {
		t.Errorf("edge selected: %+v", e)
	}
	// An idle session selected: its (few) edges appear.
	if e = edgeMap(g.edges("node:s:bbbbbbbb", false)); e["tl:0"].State != toolkit.GraphEdgeNormal {
		t.Errorf("%+v", e)
	}
	// Garbage selections draw the base picture.
	if len(g.edges("nonsense", false)) != len(g.edges("", false)) {
		t.Error("an unknown selection changed the picture")
	}
}

// Selecting changes the edges and nothing else: no cluster or node list is
// replaced, so the GraphView does not lay out again and nothing moves.
func TestSelectionChangesOnlyTheEdges(t *testing.T) {
	vm := NewViewModel()
	vm.Apply(snap())
	base := len(vm.Graph.Edges.Slice())
	var structural, edges int
	defer vm.Graph.Clusters.SubscribeChanged(func() { structural++ })()
	defer vm.Graph.Nodes.SubscribeChanged(func() { structural++ })()
	defer vm.Graph.Edges.SubscribeChanged(func() { edges++ })()
	vm.Graph.Selected.Set("node:s:aaaaaaaa")
	if structural != 0 || edges == 0 {
		t.Fatalf("a selection changed %d node/cluster lists and %d edge lists", structural, edges)
	}
	focused := 0
	for _, e := range vm.Graph.Edges.Slice() {
		if e.State == 0 { // GraphEdgeNormal
			focused++
		}
	}
	if focused < 3 || len(vm.Graph.Edges.Slice()) != base {
		t.Errorf("focus drew %d full edges of %d (was %d)", focused, len(vm.Graph.Edges.Slice()), base)
	}
	vm.Graph.Selected.Set("")
	if len(vm.Graph.Edges.Slice()) != base || structural != 0 {
		t.Error("clearing the selection must restore the base picture, still without moving anything")
	}
}

func TestBuildGraphBounds(t *testing.T) {
	s := snap()
	s.Sessions = nil
	s.Leases = nil
	s.Tells = nil
	for i := 0; i < MaxGraphSessions+3; i++ {
		id := fmt.Sprintf("%08x-0000", i)
		ag := []model.Agent{}
		for j := 0; j < MaxGraphAgents+2; j++ {
			ag = append(ag, model.Agent{ID: fmt.Sprint(j), Type: "t"})
		}
		s.Sessions = append(s.Sessions, model.Session{ID: id, Short: id[:8], HasTranscript: true, Agents: ag})
		// An expired lease first, then a live one on the same repo: live wins.
		s.Leases = append(s.Leases,
			model.Lease{Repo: fmt.Sprintf("o/r%d", i), Owner: id, Expired: true},
			model.Lease{Repo: fmt.Sprintf("o/r%d", i), Owner: id})
	}
	// More repositories than the bound, nobody holding them.
	for i := 0; i < MaxGraphRepos; i++ {
		s.Leases = append(s.Leases, model.Lease{Repo: fmt.Sprintf("x/r%d", i), Owner: s.Sessions[0].ID, Expired: true})
	}
	g := buildGraph(s, nil)
	// The 3 sessions past the bound leave their repositories unheld.
	if g.sessionsNotDrawn != 3 || g.repoNotDrawn != 3 {
		t.Errorf("not drawn: %d sessions, %d repos", g.sessionsNotDrawn, g.repoNotDrawn)
	}
	clusters, more, free, held := 0, 0, 0, 0
	for _, c := range g.clusters {
		clusters++
		_ = c
	}
	for _, n := range g.nodes {
		if strings.HasPrefix(n.ID, "m:") {
			more++
			if n.Label != "+2 more" {
				t.Errorf("more node %q", n.Label)
			}
		}
		switch n.Category {
		case "org": // folded: 200 repos of org x and 3 of org o are two nodes
			free++
		case "held repo":
			held++
		}
	}
	if clusters != MaxGraphSessions+1 || more != MaxGraphSessions || free != 2 || held != MaxGraphSessions {
		t.Errorf("clusters %d more %d free %d held %d", clusters, more, free, held)
	}
	// Nothing unheld: no "other repositories" cluster.
	s.Leases = s.Leases[:2*MaxGraphSessions]
	for _, c := range buildGraph(s, nil).clusters {
		if c.ID == reposCluster {
			t.Error("an other-repositories cluster with nothing in it")
		}
	}
}

func orgSnap() model.Snapshot {
	s := snap()
	// o/old, ghost/x are unheld already; add a second repo for org ghost, and a
	// bare name with no org.
	s.Leases = append(s.Leases,
		model.Lease{Repo: "ghost/y", Owner: idA, Expired: true},
		model.Lease{Repo: "ghost/z", Owner: idA, Expired: true},
		model.Lease{Repo: "bare", Owner: idA, Expired: true})
	return s
}

func TestUnheldReposAreFoldedByOrg(t *testing.T) {
	g := buildGraph(orgSnap(), nil)
	nodes := map[string]toolkit.GraphNode{}
	for _, n := range g.nodes {
		nodes[n.ID] = n
	}
	if n := nodes["o:ghost"]; n.Label != "+ ghost (3)" || n.Category != "org" || n.Cluster != reposCluster {
		t.Errorf("org node %+v", n)
	}
	for _, id := range []string{"r:ghost/x", "r:ghost/y", "r:ghost/z"} {
		if _, ok := nodes[id]; ok {
			t.Errorf("%s is drawn while its org is folded", id)
		}
	}
	// An org with one repository, and a repo with no org, show themselves.
	if _, ok := nodes["r:o/old"]; !ok {
		t.Error("a single-repo org needs no fold")
	}
	if _, ok := nodes["r:bare"]; !ok {
		t.Error("a repo with no org needs no fold")
	}
	// Expanded: the org node says so and its repositories appear.
	g = buildGraph(orgSnap(), map[string]bool{"ghost": true})
	nodes = map[string]toolkit.GraphNode{}
	for _, n := range g.nodes {
		nodes[n.ID] = n
	}
	if nodes["o:ghost"].Label != "- ghost (3)" {
		t.Errorf("expanded label %q", nodes["o:ghost"].Label)
	}
	for _, id := range []string{"r:ghost/x", "r:ghost/y", "r:ghost/z"} {
		if nodes[id].Cluster != reposCluster {
			t.Errorf("%s missing when expanded", id)
		}
	}
	if len(g.orgRepos["ghost"]) != 3 {
		t.Errorf("%v", g.orgRepos)
	}
	if orgOf("a/b") != "a" || orgOf("solo") != "" {
		t.Error("orgOf")
	}
}

func TestExpandingAnOrgIsViewModelStateAndEscapeCollapses(t *testing.T) {
	vm := NewViewModel()
	vm.Expanded.Append("ignored-before-a-snapshot")
	vm.CollapseAll()
	vm.Apply(orgSnap())
	has := func(id string) bool {
		for _, n := range vm.Graph.Nodes.Slice() {
			if n.ID == id {
				return true
			}
		}
		return false
	}
	if has("r:ghost/x") || !has("o:ghost") {
		t.Fatal("folded to start with")
	}
	vm.ToggleOrg("node:s:aaaaaaaa") // not an org node: ignored
	vm.ToggleOrg("node:o:ghost")
	if !has("r:ghost/x") || vm.Expanded.Len() != 1 {
		t.Fatalf("expanded: %v", vm.Expanded.Slice())
	}
	vm.Graph.Selected.Set("node:o:ghost")
	if d := vm.Detail.At(0); !strings.HasPrefix(d, "org ghost: 3 repositories nobody holds") {
		t.Errorf("detail %q", d)
	}
	if !strings.Contains(strings.Join(vm.Detail.Slice(), "\n"), "ghost/y") {
		t.Error("the org's repositories are listed in full")
	}
	vm.ToggleOrg("node:o:ghost")
	if has("r:ghost/x") || vm.Expanded.Len() != 0 {
		t.Fatal("toggled closed")
	}
	vm.ToggleOrg("node:o:ghost")
	vm.CollapseAll()
	if has("r:ghost/x") {
		t.Fatal("Escape (CollapseAll) did not fold")
	}
}

func TestShowExpiredToggleIsBoundAndRedrawsEdgesOnly(t *testing.T) {
	vm := NewViewModel()
	vm.Apply(snap())
	vm.Graph.Selected.Set("node:s:aaaaaaaa")
	has := func() bool {
		for _, e := range vm.Graph.Edges.Slice() {
			if e.ID == "ls:aaaaaaaa:o/old" {
				return true
			}
		}
		return false
	}
	if has() {
		t.Fatal("expired edge drawn by default")
	}
	structural := 0
	defer vm.Graph.Nodes.SubscribeChanged(func() { structural++ })()
	vm.ShowExpired.Set(true)
	if !has() || structural != 0 {
		t.Fatalf("toggle on: drawn %v, %d node changes", has(), structural)
	}
	vm.ShowExpired.Set(false)
	if has() {
		t.Fatal("toggle off")
	}
}

func TestRepoLabelShortensTheOrgNeverTheRepo(t *testing.T) {
	for _, tc := range []struct {
		key  string
		max  int
		want string
	}{
		{"go-authn/bridge", 22, "go-authn/bridge"}, // fits
		{"go-authn/bridge", 12, "go-a…/bridge"},    // the org gives way
		{"go-authn/bridge", 9, "g…/bridge"},        // two characters of room
		{"go-authn/bridge", 8, "…/bridge"},         // one character of room: just an ellipsis
		{"go-authn/bridge", 7, "bridge"},           // no room for an org at all
		{"go-authn/bridge", 4, "bri…"},             // not even the repo fits
		{"go-widgets/go-widgets.github.io", 22, "…/go-widgets.github.io"},
		{"a-long-org-name/repo", 10, "a-lo…/repo"},
		{"solo", 10, "solo"},
		{"solo-that-is-long", 8, "solo-th…"},
	} {
		if got := RepoLabel(tc.key, tc.max); got != tc.want {
			t.Errorf("RepoLabel(%q, %d) = %q, want %q", tc.key, tc.max, got, tc.want)
		}
	}
}
