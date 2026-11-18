package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/toolkit"
)

var t0 = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

const (
	idA = "aaaaaaaa-0000-4000-8000-000000000001"
	idB = "bbbbbbbb-0000-4000-8000-000000000002"
	idC = "cccccccc-0000-4000-8000-000000000003"
)

// snap is a small network: A is active with a lease, sub-agents and tells; B is
// idle and has only a cwd; C has no transcript.
func snap() model.Snapshot {
	return model.Snapshot{
		Version: 1, At: t0, ActiveFor: "10m0s",
		Sessions: []model.Session{
			{ID: idA, Short: "aaaaaaaa", Active: true, HasTranscript: true, Title: "A title", Cwd: "/w/a", CwdRepo: "o/a", Branch: "b", PR: "o/a#1", Project: "-p", Slug: "slug", Sent: 2, MemAdds: 3,
				LastActivity: t0.Add(-time.Minute), Agents: []model.Agent{
					{ID: "ag1", Type: "general-purpose", Description: "fix the build", ToolUseID: "toolu_1", Depth: 1, Active: true, LastActivity: t0},
					{ID: "ag2", Type: "", Description: "idle one", LastActivity: t0.Add(-3 * time.Hour)},
				}},
			{ID: idB, Short: "bbbbbbbb", HasTranscript: true, CwdRepo: "o/b", LastActivity: t0.Add(-2 * time.Hour)},
			{ID: idC, Short: "cccccccc", LastActivity: t0.Add(-72 * time.Hour)},
		},
		Leases: []model.Lease{
			{Resource: "o/a", Repo: "o/a", Kind: model.KindRepo, Path: "/clones/a", Owner: idA, Note: "working on a", Acquired: t0.Add(-time.Hour), Expires: t0.Add(time.Hour)},
			{Resource: "o/old", Repo: "o/old", Kind: model.KindRepoForm, Owner: idA, Note: "done", Acquired: t0.Add(-5 * time.Hour), Expires: t0.Add(-4 * time.Hour), Expired: true},
			{Resource: "o/a", Repo: "o/a", Owner: idA, Note: "older duplicate", Expired: true, Expires: t0.Add(-time.Hour)},
			{Resource: "ghost/x", Repo: "ghost/x", Owner: "dddddddd-0000", Note: "owner not drawn"},
		},
		Tells: []model.Tell{
			{Target: "bbbbbbbb", TargetKind: model.TargetSession, From: idA, Text: "hello b", At: t0},
			{Target: "o/a", TargetKind: model.TargetResource, From: idB, Text: "about a", At: t0.Add(-time.Minute)},
			{Target: "o/nowhere", TargetKind: model.TargetResource, From: idB, Text: "nowhere", At: t0.Add(-time.Hour)},
			{Target: "dddddddd", TargetKind: model.TargetSession, From: idA, Text: "to an undrawn", At: t0.Add(-2 * time.Hour)},
		},
		Anomalies: []model.Anomaly{
			{Kind: model.AnomalyUnresolvedKey, Subject: "--note", Detail: "owner b1f9b4f5"},
			{Kind: model.AnomalyBadLine, Subject: "transcripts"},
		},
		Stats: model.Stats{TranscriptsRead: 2, TranscriptsSeen: 9, BytesRead: 4096, LeasesRead: 4, LeasesTorn: 1, TellsRead: 4, TellsDropped: 1, ExpiredNotListed: 7, ElapsedMicros: 15000},
	}
}

func TestBuildGraphMapping(t *testing.T) {
	g := buildGraph(snap())
	clusters := map[string]string{}
	for _, c := range g.clusters {
		clusters[c.ID] = c.Label
	}
	if clusters["aaaaaaaa"] != "aaaaaaaa o/a" || clusters["bbbbbbbb"] != "bbbbbbbb o/b" || clusters["cccccccc"] != "cccccccc" || clusters[reposCluster] != repoClusterLabel {
		t.Errorf("clusters %v", clusters)
	}
	cat := map[string]string{}
	for _, n := range g.nodes {
		cat[n.ID] = n.Category
	}
	for id, want := range map[string]string{
		"s:aaaaaaaa": "session", "s:bbbbbbbb": "idle session", "s:cccccccc": "no transcript",
		"a:aaaaaaaa:ag1": "general-purpose", "a:aaaaaaaa:ag2": "agent", "r:o/a": "repo", "r:o/old": "repo", "r:ghost/x": "repo",
	} {
		if cat[id] != want {
			t.Errorf("node %s category %q, want %q", id, cat[id], want)
		}
	}
	edges := map[string]toolkit.GraphEdge{}
	for _, e := range g.edges {
		edges[e.ID] = e
	}
	if e := edges["sp:a:aaaaaaaa:ag1"]; e.From != "s:aaaaaaaa" || e.To != "a:aaaaaaaa:ag1" || e.State != toolkit.GraphEdgeNormal || e.Label != "fix the build" {
		t.Errorf("spawn edge %+v", e)
	}
	if e := edges["sp:a:aaaaaaaa:ag2"]; e.State != toolkit.GraphEdgeIdle {
		t.Errorf("an idle sub-agent's spawn edge is Idle: %+v", e)
	}
	if e := edges["ls:aaaaaaaa:o/a"]; e.State != toolkit.GraphEdgeNormal || e.Label != "working on a" {
		t.Errorf("live lease edge %+v", e)
	}
	if e := edges["ls:aaaaaaaa:o/old"]; e.State != toolkit.GraphEdgeIdle {
		t.Errorf("an expired lease's edge is Idle: %+v", e)
	}
	if _, ok := edges["ls:dddddddd:ghost/x"]; ok {
		t.Error("an edge from a session that is not drawn")
	}
	if e, ok := edges["tl:0"]; !ok || e.To != "s:bbbbbbbb" {
		t.Errorf("tell to a session: %+v", e)
	}
	if e, ok := edges["tl:1"]; !ok || e.To != "r:o/a" || e.From != "s:bbbbbbbb" {
		t.Errorf("tell to a repo: %+v", e)
	}
	if _, ok := edges["tl:2"]; ok {
		t.Error("a tell to a repo that is not drawn")
	}
	if _, ok := edges["tl:3"]; ok {
		t.Error("a tell to a session that is not drawn")
	}
	n := 0
	for _, e := range g.edges {
		if strings.HasPrefix(e.ID, "ls:aaaaaaaa:o/a") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("a repo held twice by one session has %d lease edges", n)
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
	// More repositories than the bound, all held by the first session.
	for i := 0; i < MaxGraphRepos; i++ {
		s.Leases = append(s.Leases, model.Lease{Repo: fmt.Sprintf("x/r%d", i), Owner: s.Sessions[0].ID})
	}
	g := buildGraph(s)
	if g.sessionsNotDrawn != 3 || g.repoNotDrawn != MaxGraphSessions+3 {
		t.Errorf("not drawn: %d sessions, %d repos", g.sessionsNotDrawn, g.repoNotDrawn)
	}
	clusters, more, repos := 0, 0, 0
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
		if n.Cluster == reposCluster {
			repos++
		}
	}
	if clusters != MaxGraphSessions+1 || more != MaxGraphSessions || repos != MaxGraphRepos {
		t.Errorf("clusters %d more %d repos %d", clusters, more, repos)
	}
	// No leases at all: no repositories cluster.
	s.Leases = nil
	for _, c := range buildGraph(s).clusters {
		if c.ID == reposCluster {
			t.Error("a repositories cluster with nothing in it")
		}
	}
}

func TestRepoNameAndAgo(t *testing.T) {
	if repoName("o/r") != "r" || repoName("solo") != "solo" {
		t.Error("repoName")
	}
	for d, want := range map[time.Duration]string{0: "now", 30 * time.Second: "now", 5 * time.Minute: "5m", 3 * time.Hour: "3h", 49 * time.Hour: "2d"} {
		if got := ago(t0, t0.Add(-d)); got != want {
			t.Errorf("ago(%v) = %q, want %q", d, got, want)
		}
	}
	if ago(t0, time.Time{}) != "-" {
		t.Error("zero time")
	}
}

func TestViewModelApplyFiltersAndSelects(t *testing.T) {
	vm := NewViewModel()
	if _, ok := vm.Snapshot(); ok {
		t.Error("nothing applied yet")
	}
	if vm.Detail.Len() != 0 {
		t.Error("no detail before a snapshot")
	}
	vm.Apply(snap())
	if s, ok := vm.Snapshot(); !ok || len(s.Sessions) != 3 {
		t.Fatal("snapshot")
	}
	if vm.Rows.Len() != 3 {
		t.Fatalf("rows %d", vm.Rows.Len())
	}
	r := vm.Rows.At(0)
	if r.ID != "aaaaaaaa" || r.State != "ACTIVE" || r.Repo != "o/a" || r.Task != "working on a" || r.Agent != "2" || r.Last != "1m" {
		t.Errorf("row %+v", r)
	}
	if vm.Rows.At(1).State != "idle" || vm.Rows.At(1).Repo != "o/b" || vm.Rows.At(2).State != "no tr." {
		t.Errorf("rows %+v", vm.Rows.Slice())
	}
	if cells := r.Cells(); len(cells) != 6 || cells[1] != "aaaaaaaa" {
		t.Errorf("cells %v", cells)
	}
	if !strings.Contains(vm.Status.Get(), "3 sessions (1 active in 10m0s) | 2 sub-agents | 3 repos (2 held) | 4 tells | 2 anomalies | 12:00:00") {
		t.Errorf("status %q", vm.Status.Get())
	}
	// The first line of the detail pane, with nothing selected, is the anomalies.
	if first := vm.Detail.At(0); !strings.HasPrefix(first, "ANOMALIES (2)") {
		t.Errorf("detail %q", first)
	}

	// The query narrows the table and dims the graph.
	vm.Query.Set("o/b")
	if vm.Rows.Len() != 1 || vm.Rows.At(0).ID != "bbbbbbbb" || vm.Graph.Filter.Get() != "o/b" {
		t.Errorf("rows %d filter %q", vm.Rows.Len(), vm.Graph.Filter.Get())
	}
	vm.Query.Set("")
	if vm.Rows.Len() != 3 {
		t.Error("clearing the query restores the rows")
	}

	// Selecting a node selects its row; selecting a row selects its node.
	vm.Graph.Selected.Set("node:s:bbbbbbbb")
	if vm.TableSel.Get() != 1 {
		t.Errorf("table selection %d", vm.TableSel.Get())
	}
	vm.TableSel.Set(2)
	if vm.Graph.Selected.Get() != "node:s:cccccccc" {
		t.Errorf("graph selection %q", vm.Graph.Selected.Get())
	}
	vm.TableSel.Set(-1) // clearing the row does not touch the node
	vm.TableSel.Set(99)
	if vm.Graph.Selected.Get() != "node:s:cccccccc" {
		t.Errorf("out-of-range rows select nothing: %q", vm.Graph.Selected.Get())
	}
	// A selection the query hides is no row.
	vm.Graph.Selected.Set("node:s:aaaaaaaa")
	vm.Query.Set("bbbb")
	if vm.TableSel.Get() != -1 {
		t.Errorf("table selection %d for a hidden row", vm.TableSel.Get())
	}
	// A selection on something that is not a session selects no row.
	vm.Graph.Selected.Set("node:r:o/a")
	if vm.TableSel.Get() != -1 {
		t.Error("a repo is no row")
	}
	vm.Query.Set("")

	// An identical snapshot replaces nothing: the graph must not lay out again.
	var changes int
	for _, u := range []func(){
		vm.Graph.Clusters.SubscribeChanged(func() { changes++ }),
		vm.Graph.Nodes.SubscribeChanged(func() { changes++ }),
		vm.Graph.Edges.SubscribeChanged(func() { changes++ }),
		vm.Rows.SubscribeChanged(func() { changes++ }),
	} {
		defer u()
	}
	vm.Apply(snap())
	if changes != 0 {
		t.Errorf("an unchanged snapshot changed %d lists", changes)
	}
	s2 := snap()
	s2.Tells = s2.Tells[:1]
	vm.Apply(s2)
	if changes == 0 {
		t.Error("a changed snapshot changed nothing")
	}
	// Same number of items, different content: still replaced.
	vm.Apply(snap())
	changes = 0
	s3 := snap()
	s3.Tells[0].Text = "a different text"
	vm.Apply(s3)
	if changes == 0 {
		t.Error("a changed edge label changed nothing")
	}
}

func TestViewModelNotDrawnNote(t *testing.T) {
	vm := NewViewModel()
	s := snap()
	for i := 0; i < MaxGraphRepos+2; i++ {
		s.Leases = append(s.Leases, model.Lease{Repo: fmt.Sprintf("x/r%d", i), Owner: idA})
	}
	vm.Apply(s)
	if !strings.Contains(vm.Status.Get(), "not drawn: 0 sessions, ") || !strings.Contains(vm.Status.Get(), "repos (see the table)") {
		t.Errorf("status %q", vm.Status.Get())
	}
}

func TestDetailLines(t *testing.T) {
	s := snap()
	join := func(sel string) string { return strings.Join(detailLines(s, sel), "\n") }

	a := join("node:s:aaaaaaaa")
	for _, want := range []string{"session " + idA, "ACTIVE", "task: working on a", "title: A title", "slug: slug", "cwd: /w/a", "cwd repo: o/a", "branch: b", "pull request: o/a#1",
		"project: -p", "sent 2 SendMessage", "3 memory entries", "leases (3):", "o/a [held until 13:00]", "o/old [expired 4h ago] done", "sub-agents (2):",
		"general-purpose [ACTIVE] fix the build", "[3h] idle one", "tell sent to bbbbbbbb: hello b", "tell sent to dddddddd"} {
		if !strings.Contains(a, want) {
			t.Errorf("session detail lacks %q:\n%s", want, a)
		}
	}
	b := join("node:s:bbbbbbbb")
	if !strings.Contains(b, "idle") || !strings.Contains(b, "tell from aaaaaaaa: hello b") || !strings.Contains(b, "tell sent to o/a: about a") {
		t.Errorf("%s", b)
	}
	if !strings.Contains(join("node:s:cccccccc"), "no transcript found") {
		t.Error("no transcript")
	}
	if !strings.Contains(join("node:s:zzzzzzzz"), "not in this snapshot") {
		t.Error("unknown session")
	}

	if g := join("node:a:aaaaaaaa:ag1"); !strings.Contains(g, "sub-agent ag1") || !strings.Contains(g, "ACTIVE") || !strings.Contains(g, "spawned by session aaaaaaaa") {
		t.Errorf("%s", g)
	}
	if g := join("node:a:aaaaaaaa:ag2"); !strings.Contains(g, "idle, last 3h ago") {
		t.Errorf("%s", g)
	}
	if !strings.Contains(join("node:a:aaaaaaaa:nope"), "not in this snapshot") {
		t.Error("unknown agent")
	}

	if r := join("node:r:o/a"); !strings.Contains(r, "repository o/a") || !strings.Contains(r, "kind: repo") || !strings.Contains(r, "clone: /clones/a") || !strings.Contains(r, "lease by aaaaaaaa [held until 13:00] working on a") || !strings.Contains(r, "tell from bbbbbbbb: about a") {
		t.Errorf("%s", r)
	}
	if r := join("node:r:o/old"); strings.Contains(r, "clone:") || !strings.Contains(r, "expired 4h ago") {
		t.Errorf("%s", r)
	}
	if r := join("node:r:o/none"); r != "repository o/none" {
		t.Errorf("%q", r)
	}

	if m := join("node:m:aaaaaaaa"); !strings.Contains(m, "not drawn") || !strings.Contains(m, "listed 2") {
		t.Errorf("%s", m)
	}

	if e := join("edge:tl:0"); !strings.Contains(e, "tell aaaaaaaa -> bbbbbbbb (session)") || !strings.Contains(e, "hello b") {
		t.Errorf("%s", e)
	}
	for _, bad := range []string{"edge:tl:99", "edge:tl:x", "edge:tl:-1"} {
		if !strings.Contains(join(bad), "not in this snapshot") {
			t.Errorf("%s", bad)
		}
	}
	if e := join("edge:ls:aaaaaaaa:o/a"); !strings.Contains(e, "lease o/a held by aaaaaaaa") || !strings.Contains(e, "note: working on a") {
		t.Errorf("%s", e)
	}
	if !strings.Contains(join("edge:ls:aaaaaaaa:o/zzz"), "lease: not in this snapshot") {
		t.Error("unknown lease edge")
	}
	if e := join("edge:sp:a:aaaaaaaa:ag1"); !strings.Contains(e, "sub-agent ag1") {
		t.Errorf("%s", e)
	}
	if join("edge:weird") != "edge weird" {
		t.Error("unknown edge")
	}

	an := join("")
	for _, want := range []string{"ANOMALIES (2): shown, not hidden", "unresolved key: --note - owner b1f9b4f5", "unparsable line: transcripts", "read: 2 of 9 transcripts", "(1 torn)", "not listed: 7", "collected in 15 ms"} {
		if !strings.Contains(an, want) {
			t.Errorf("anomaly lines lack %q:\n%s", want, an)
		}
	}
}

func TestWrapAndDetailBound(t *testing.T) {
	long := strings.Repeat("word ", 40)
	got := wrap([]string{"short", long, strings.Repeat("x", 200)}, 30)
	if got[0] != "short" {
		t.Error("short lines pass through")
	}
	for _, l := range got {
		if len([]rune(l)) > 32 {
			t.Errorf("line of %d runes: %q", len([]rune(l)), l)
		}
	}
	if !strings.HasPrefix(got[2], "  ") {
		t.Errorf("continuations are indented: %q", got[2])
	}
	vm := NewViewModel()
	s := snap()
	for i := 0; i < 2*maxDetailLines; i++ {
		s.Anomalies = append(s.Anomalies, model.Anomaly{Kind: "k", Subject: fmt.Sprint(i)})
	}
	vm.Apply(s)
	if vm.Detail.Len() != maxDetailLines+1 || !strings.HasPrefix(vm.Detail.At(maxDetailLines), "... ") {
		t.Errorf("the detail pane must say what it cut: %d lines", vm.Detail.Len())
	}
}
