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
	g := buildGraph(snap())
	clusters := map[string]string{}
	for _, c := range g.clusters {
		clusters[c.ID] = c.Label
	}
	if clusters["aaaaaaaa"] != "aaaaaaaa o/a" || clusters["bbbbbbbb"] != "bbbbbbbb o/b" || clusters["cccccccc"] != "cccccccc" || clusters[reposCluster] != repoClusterLabel {
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
	g := buildGraph(s)
	labels := map[string]string{}
	for _, n := range g.nodes {
		labels[n.ID] = n.Label
	}
	if labels["a:aaaaaaaa:q"] != "Explore" || labels["a:aaaaaaaa:z"] != "agent" {
		t.Errorf("%v", labels)
	}
	long := strings.Repeat("word ", 20)
	s.Sessions[0].Agents = []model.Agent{{ID: "l", Type: "t", Description: long}}
	for _, n := range buildGraph(s).nodes {
		if n.ID == "a:aaaaaaaa:l" && len([]rune(n.Label)) > NodeChars {
			t.Errorf("label of %d runes: %q", len([]rune(n.Label)), n.Label)
		}
	}
}

func TestFocusModeEdges(t *testing.T) {
	g := buildGraph(snap())

	// Nothing selected: only the edges of ACTIVE sessions, dimmed; tells in full.
	e := edgeMap(g.edges(""))
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

	// A session selected: its whole cluster is in focus, expired history too.
	e = edgeMap(g.edges("node:s:aaaaaaaa"))
	for _, id := range []string{"sp:a:aaaaaaaa:ag1", "sp:a:aaaaaaaa:ag2", "ls:aaaaaaaa:o/a", "ls:aaaaaaaa:o/old"} {
		if e[id].State != toolkit.GraphEdgeNormal {
			t.Errorf("%s with its session selected: %+v", id, e[id])
		}
	}
	// A repository selected: its edge in full, the rest dimmed.
	e = edgeMap(g.edges("node:r:o/a"))
	if e["ls:aaaaaaaa:o/a"].State != toolkit.GraphEdgeNormal || e["sp:a:aaaaaaaa:ag1"].State != toolkit.GraphEdgeIdle {
		t.Errorf("repo selected: %+v %+v", e["ls:aaaaaaaa:o/a"], e["sp:a:aaaaaaaa:ag1"])
	}
	// A sub-agent selected.
	e = edgeMap(g.edges("node:a:aaaaaaaa:ag1"))
	if e["sp:a:aaaaaaaa:ag1"].State != toolkit.GraphEdgeNormal || e["sp:a:aaaaaaaa:ag2"].State != toolkit.GraphEdgeIdle {
		t.Errorf("agent selected: %+v", e)
	}
	// An edge selected is drawn in full even when nothing would draw it.
	e = edgeMap(g.edges("edge:ls:aaaaaaaa:o/old"))
	if e["ls:aaaaaaaa:o/old"].State != toolkit.GraphEdgeNormal || e["ls:aaaaaaaa:o/a"].State != toolkit.GraphEdgeIdle {
		t.Errorf("edge selected: %+v", e)
	}
	// An idle session selected: its (few) edges appear.
	if e = edgeMap(g.edges("node:s:bbbbbbbb")); e["tl:0"].State != toolkit.GraphEdgeNormal {
		t.Errorf("%+v", e)
	}
	// Garbage selections draw the base picture.
	if len(g.edges("nonsense")) != len(g.edges("")) {
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
	if focused < 3 || len(vm.Graph.Edges.Slice()) <= base {
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
	g := buildGraph(s)
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
		case "repo":
			free++
		case "held repo":
			held++
		}
	}
	if clusters != MaxGraphSessions+1 || more != MaxGraphSessions || free != MaxGraphRepos || held != MaxGraphSessions {
		t.Errorf("clusters %d more %d free %d held %d", clusters, more, free, held)
	}
	// Nothing unheld: no "other repositories" cluster.
	s.Leases = s.Leases[:2*MaxGraphSessions]
	for _, c := range buildGraph(s).clusters {
		if c.ID == reposCluster {
			t.Error("an other-repositories cluster with nothing in it")
		}
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
