package ui

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/toolkit"
)

// The GraphView sizes each cluster by its own members and scrolls when they
// overflow, so these are safety bounds on what is drawn, not a fit to the
// screen. They are the screen's, not the collector's: the table and the detail
// pane list everything.
const (
	// MaxGraphSessions is how many session clusters are drawn, newest first.
	MaxGraphSessions = 64
	// MaxGraphAgents is how many sub-agent nodes a session cluster shows; the
	// rest are one "+N more" node.
	MaxGraphAgents = 6
	// MaxGraphRepos is how many repositories the "other repositories" cluster
	// shows (held repositories are always drawn, inside their holder).
	MaxGraphRepos = 200

	// NodeWidth is the width of a node box, and NodeChars the number of
	// characters a label is cut to so that it fits in it.
	NodeWidth = 170
	NodeChars = 22
)

// Node and edge id prefixes. The ids are what the graph's Selected observable
// carries ("node:" + id), and what the detail pane parses back.
const (
	sessionPrefix = "s:"
	agentPrefix   = "a:"
	repoPrefix    = "r:"
	morePrefix    = "m:"
	leasePrefix   = "ls:"
	spawnPrefix   = "sp:"
	tellPrefix    = "tl:"
	reposCluster  = "repos"
)

// repoClusterLabel is the label of the cluster of repositories nobody holds.
const repoClusterLabel = "other repositories"

// candidate is an edge the graph MAY draw, with what decides whether it is.
type candidate struct {
	edge    toolkit.GraphEdge
	session string // the session it belongs to (short id); "" for a tell
	tell    bool
	expired bool // a lease edge whose lease has expired
}

// graphData is the snapshot as the GraphView wants it. The clusters and the
// nodes depend on the snapshot alone; the edges depend on the selection too
// (see edges), which is why they are kept as candidates.
type graphData struct {
	clusters []toolkit.GraphCluster
	nodes    []toolkit.GraphNode
	cands    []candidate
	// nodeCluster is node id -> cluster id; active is session short id -> active.
	nodeCluster map[string]string
	active      map[string]bool
	// notDrawn counts what the bounds above left off the picture.
	sessionsNotDrawn, repoNotDrawn int
}

// buildGraph maps a snapshot onto clusters, nodes and candidate edges.
//
//	cluster  one per session ("<short id> <repo>"), plus "other repositories"
//	node     the session's main agent, its sub-agents (labelled by their task,
//	         coloured by their type), and every repository it HOLDS, so a held
//	         repository sits next to its holder; repositories nobody holds are
//	         in the one other cluster
//	edge     session -> sub-agent (spawn), session -> repository (lease),
//	         sender -> target (tell)
func buildGraph(s model.Snapshot) graphData {
	g := graphData{nodeCluster: map[string]string{}, active: map[string]bool{}}
	drawn := map[string]bool{}
	sessions := s.Sessions
	if len(sessions) > MaxGraphSessions {
		g.sessionsNotDrawn = len(sessions) - MaxGraphSessions
		sessions = sessions[:MaxGraphSessions]
	}
	sessionDrawn := map[string]bool{}
	for _, se := range sessions {
		sessionDrawn[se.Short] = true
		g.active[se.Short] = se.Active
	}

	// Who holds each repository now (the first live lease; leases are listed
	// live first), and every repository in the order it first appears.
	holder := map[string]string{}
	var repoOrder []string
	seen := map[string]bool{}
	for _, l := range s.Leases {
		if !seen[l.Repo] {
			seen[l.Repo] = true
			repoOrder = append(repoOrder, l.Repo)
		}
		if sh := model.ShortID(l.Owner); !l.Expired && holder[l.Repo] == "" && sessionDrawn[sh] {
			holder[l.Repo] = sh
		}
	}
	var held, free []string
	for _, k := range repoOrder {
		if holder[k] != "" {
			held = append(held, k)
		} else {
			free = append(free, k)
		}
	}
	sort.SliceStable(free, func(i, j int) bool { return free[i] < free[j] })
	if len(free) > MaxGraphRepos {
		g.repoNotDrawn = len(free) - MaxGraphRepos
		free = free[:MaxGraphRepos]
	}

	add := func(n toolkit.GraphNode) {
		g.nodes = append(g.nodes, n)
		g.nodeCluster[n.ID] = n.Cluster
		drawn[n.ID] = true
	}
	heldBy := map[string][]string{}
	for _, k := range held {
		heldBy[holder[k]] = append(heldBy[holder[k]], k)
	}

	for _, se := range sessions {
		cat := "session"
		if !se.Active {
			cat = "idle session"
		}
		if !se.HasTranscript {
			cat = "no transcript"
		}
		label := se.Short
		if r := sessionRepo(s, se); r != "" {
			label += " " + r
		}
		g.clusters = append(g.clusters, toolkit.GraphCluster{ID: se.Short, Label: model.Truncate(label, 36)})
		main := sessionPrefix + se.Short
		add(toolkit.GraphNode{ID: main, Label: se.Short, Cluster: se.Short, Category: cat})
		for i, a := range se.Agents {
			if i >= MaxGraphAgents {
				add(toolkit.GraphNode{ID: morePrefix + se.Short, Label: "+" + strconv.Itoa(len(se.Agents)-MaxGraphAgents) + " more",
					Cluster: se.Short, Category: "more sub-agents"})
				break
			}
			id := agentPrefix + se.Short + ":" + a.ID
			ty := a.Type
			if ty == "" {
				ty = "agent"
			}
			label := a.Description
			if label == "" {
				label = ty
			}
			add(toolkit.GraphNode{ID: id, Label: model.Truncate(label, NodeChars), Cluster: se.Short, Category: ty})
			st := toolkit.GraphEdgeNormal
			g.cands = append(g.cands, candidate{session: se.Short,
				edge: toolkit.GraphEdge{ID: spawnPrefix + id, From: main, To: id, Label: model.Truncate(a.Description, 80), State: st}})
		}
		for _, k := range heldBy[se.Short] {
			add(toolkit.GraphNode{ID: repoPrefix + k, Label: RepoLabel(k, NodeChars), Cluster: se.Short, Category: "held repo"})
		}
	}
	if len(free) > 0 {
		g.clusters = append(g.clusters, toolkit.GraphCluster{ID: reposCluster, Label: repoClusterLabel})
		for _, k := range free {
			add(toolkit.GraphNode{ID: repoPrefix + k, Label: RepoLabel(k, NodeChars), Cluster: reposCluster, Category: "repo"})
		}
	}

	leaseEdge := map[string]bool{}
	for _, l := range s.Leases { // live first, so a live lease wins its edge
		sh := model.ShortID(l.Owner)
		from, to := sessionPrefix+sh, repoPrefix+l.Repo
		id := leasePrefix + sh + ":" + l.Repo
		if !drawn[from] || !drawn[to] || leaseEdge[id] {
			continue
		}
		leaseEdge[id] = true
		g.cands = append(g.cands, candidate{session: sh, expired: l.Expired,
			edge: toolkit.GraphEdge{ID: id, From: from, To: to, Label: model.Truncate(l.Note, 80)}})
	}
	for i, t := range s.Tells {
		from := sessionPrefix + model.ShortID(t.From)
		to := repoPrefix + t.Target
		if t.TargetKind == model.TargetSession {
			to = sessionPrefix + t.Target
		}
		if !drawn[from] || !drawn[to] {
			continue
		}
		g.cands = append(g.cands, candidate{tell: true,
			edge: toolkit.GraphEdge{ID: tellPrefix + strconv.Itoa(i), From: from, To: to, Label: model.Truncate(t.Text, 80), Weight: 100}})
	}
	return g
}

// edges is the edge list to draw for a selection (the graph's Selected value:
// "node:<id>", "edge:<id>" or "").
//
// With nothing selected only the edges of ACTIVE sessions are drawn, and
// dimmed, so the picture says who is working without a tangle of history. A
// tell is always drawn in full: there are few and each is a message. Selecting a
// node draws its edges in full and keeps the rest dimmed; selecting a session
// (its main node) counts as selecting its whole cluster, repositories it holds
// included. Selecting an edge draws it in full. Edge changes never move a node
// (the GraphView lays out on cluster and node changes only).
func (g graphData) edges(sel string) []toolkit.GraphEdge {
	focus := map[string]bool{}
	edgeSel := ""
	switch {
	case strings.HasPrefix(sel, "node:"):
		id := strings.TrimPrefix(sel, "node:")
		if strings.HasPrefix(id, sessionPrefix) {
			cl := g.nodeCluster[id]
			for n, c := range g.nodeCluster {
				if c == cl {
					focus[n] = true
				}
			}
		}
		focus[id] = true
	case strings.HasPrefix(sel, "edge:"):
		edgeSel = strings.TrimPrefix(sel, "edge:")
	}
	var out []toolkit.GraphEdge
	for _, c := range g.cands {
		e := c.edge
		switch {
		case c.tell:
			e.State = toolkit.GraphEdgeNormal
		case focus[e.From] || focus[e.To] || e.ID == edgeSel:
			e.State = toolkit.GraphEdgeNormal
		case g.active[c.session] && !c.expired:
			e.State = toolkit.GraphEdgeIdle
		default:
			continue
		}
		out = append(out, e)
	}
	return out
}

// sessionRepo is the repository to name a session by: the first live lease's,
// else the one its working directory is inside.
func sessionRepo(s model.Snapshot, se model.Session) string {
	for _, l := range s.LeasesOf(se.ID) {
		if !l.Expired {
			return l.Repo
		}
	}
	return se.CwdRepo
}

// RepoLabel is org/repo cut to at most max characters. When it does not fit,
// the ORG is shortened (with an ellipsis) and the repo is kept whole; only a
// repo name longer than the room itself is cut.
func RepoLabel(key string, max int) string {
	org, repo, ok := strings.Cut(key, "/")
	if !ok {
		return model.Truncate(key, max)
	}
	if len([]rune(key)) <= max {
		return key
	}
	room := max - len([]rune(repo)) - 1 // the "/" and the repo stay
	switch {
	case room >= 2:
		return model.Truncate(org, room) + "/" + repo
	case room == 1:
		return "…/" + repo
	}
	return model.Truncate(repo, max)
}

// ago renders the age of t at now: "now", "4m", "3h", "2d".
func ago(now, t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	switch {
	case d < 45*time.Second:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	}
	return strconv.Itoa(int(d.Hours()/24)) + "d"
}
