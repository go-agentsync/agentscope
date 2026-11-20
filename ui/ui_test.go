package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
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
				Model:        "claude-opus-5-5",
				Models:       []model.ModelSeen{{ID: "claude-opus-5-5", Last: t0}, {ID: "claude-sonnet-5-5", Last: t0.Add(-2 * time.Hour)}},
				LastActivity: t0.Add(-time.Minute), Agents: []model.Agent{
					{ID: "ag1", Type: "general-purpose", Description: "fix the build", ToolUseID: "toolu_1", Depth: 1, Active: true, LastActivity: t0, Model: "claude-sonnet-5-5"},
					{ID: "ag2", Type: "", Description: "idle one", LastActivity: t0.Add(-3 * time.Hour)},
				}},
			{ID: idB, Short: "bbbbbbbb", HasTranscript: true, CwdRepo: "o/b", Model: "claude-mystery-9", LastActivity: t0.Add(-2 * time.Hour)},
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

func TestRepoNameAndAgo(t *testing.T) {
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
	if cells := r.Cells(); len(cells) != 8 || cells[1] != "aaaaaaaa" || cells[3] != "Opus 5.5" || cells[2] != "- / 1M" {
		t.Errorf("cells %v", cells)
	}
	if !strings.Contains(vm.Status.Get(), "3 of 3 sessions shown (1 active in 10m0s) | 2 sub-agents | 3 repos (2 held) | 4 tells | 2 anomalies") {
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
		s.Leases = append(s.Leases, model.Lease{Repo: fmt.Sprintf("x/r%d", i), Owner: idA, Expired: true})
	}
	vm.Apply(s)
	// 2 over the bound, plus o/old (expired) and ghost/x (holder not drawn).
	if !strings.Contains(vm.Status.Get(), "not drawn: 0 sessions, 4 repos (see the table)") {
		t.Errorf("status %q", vm.Status.Get())
	}
}

func TestDetailLines(t *testing.T) {
	s := snap()
	join := func(sel string) string { return strings.Join(detailLines(s, sel), "\n") }

	a := join("node:s:aaaaaaaa")
	for _, want := range []string{"session " + idA, "ACTIVE", "task: working on a", "title: A title", "slug: slug", "cwd: /w/a", "cwd repo: o/a", "branch: b", "pull request: o/a#1",
		"project: -p", "sent 2 SendMessage", "3 memory entries", "leases (3):", "o/a [held until 13:00]", "o/old [expired 4h ago] done", "sub-agents (2):",
		"general-purpose [ACTIVE, Sonnet 5.5] fix the build", "[3h, ?] idle one", "tell sent to bbbbbbbb: hello b", "tell sent to dddddddd",
		"model: Opus 5.5 (claude-opus-5-5)", "models seen (newest first):", "  Sonnet 5.5 (claude-sonnet-5-5), last 2h ago"} {
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

	if !strings.Contains(b, "model: ? (claude-mystery-9)") {
		t.Errorf("an unknown model is shown as ? with its raw id:\n%s", b)
	}
	if g := join("node:a:aaaaaaaa:ag1"); !strings.Contains(g, "model: Sonnet 5.5 (claude-sonnet-5-5)") || !strings.Contains(g, "sub-agent ag1") || !strings.Contains(g, "ACTIVE") || !strings.Contains(g, "spawned by session aaaaaaaa") {
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
	for _, want := range []string{"ANOMALIES (2): shown, not hidden", "unresolved key: --note - owner b1f9b4f5", "unparsable line: transcripts", "read: 9 transcripts", "(1 torn)", "not listed: 7"} {
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
