package ui

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/toolkit"
)

// The GraphView lays every cluster out at the size of the largest one, so the
// number of members a cluster may have is what keeps the picture on one
// screen. These bounds are the screen's, not the collector's: the table and
// the detail pane list everything.
const (
	// MaxGraphSessions is how many session clusters are drawn, newest first.
	MaxGraphSessions = 32
	// MaxGraphAgents is how many sub-agent nodes a session cluster shows; the
	// rest are one "+N more" node.
	MaxGraphAgents = 3
	// MaxGraphRepos is how many repositories the repositories cluster shows,
	// live leases first.
	MaxGraphRepos = 9
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

// graphData is the snapshot as the GraphView wants it.
type graphData struct {
	clusters []toolkit.GraphCluster
	nodes    []toolkit.GraphNode
	edges    []toolkit.GraphEdge
	// notDrawn counts what the bounds above left off the picture.
	sessionsNotDrawn, repoNotDrawn int
}

// repoClusterLabel is the label of the repositories cluster.
const repoClusterLabel = "repositories"

// buildGraph maps a snapshot onto clusters, nodes and edges.
//
//	cluster  one per session ("<short> <repo>"), plus "repositories"
//	node     the session's main agent, its sub-agents (category = agent type),
//	         and one node per repository held
//	edge     session -> sub-agent (spawn), session -> repository (lease; an
//	         expired lease is Idle), sender -> target (tell)
func buildGraph(s model.Snapshot) graphData {
	var g graphData
	drawn := map[string]bool{}
	sessions := s.Sessions
	if len(sessions) > MaxGraphSessions {
		g.sessionsNotDrawn = len(sessions) - MaxGraphSessions
		sessions = sessions[:MaxGraphSessions]
	}

	// Repositories: live leases first, then the most recent expired ones.
	var repoOrder []string
	repoState := map[string]bool{} // key -> live
	for _, l := range s.Leases {
		live, seen := repoState[l.Repo]
		if seen && (live || l.Expired) {
			continue
		}
		if !seen {
			repoOrder = append(repoOrder, l.Repo)
		}
		repoState[l.Repo] = !l.Expired
	}
	sort.SliceStable(repoOrder, func(i, j int) bool { return repoState[repoOrder[i]] && !repoState[repoOrder[j]] })
	if len(repoOrder) > MaxGraphRepos {
		g.repoNotDrawn = len(repoOrder) - MaxGraphRepos
		repoOrder = repoOrder[:MaxGraphRepos]
	}
	repoDrawn := map[string]bool{}
	for _, k := range repoOrder {
		repoDrawn[k] = true
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
		g.nodes = append(g.nodes, toolkit.GraphNode{ID: main, Label: se.Short, Cluster: se.Short, Category: cat})
		drawn[main] = true
		for i, a := range se.Agents {
			if i >= MaxGraphAgents {
				g.nodes = append(g.nodes, toolkit.GraphNode{
					ID: morePrefix + se.Short, Label: "+" + strconv.Itoa(len(se.Agents)-MaxGraphAgents) + " more",
					Cluster: se.Short, Category: "more sub-agents"})
				break
			}
			id := agentPrefix + se.Short + ":" + a.ID
			ty := a.Type
			if ty == "" {
				ty = "agent"
			}
			g.nodes = append(g.nodes, toolkit.GraphNode{ID: id, Label: model.Truncate(ty, 12), Cluster: se.Short, Category: ty})
			st := toolkit.GraphEdgeNormal
			if !a.Active {
				st = toolkit.GraphEdgeIdle
			}
			g.edges = append(g.edges, toolkit.GraphEdge{ID: spawnPrefix + id, From: main, To: id, Label: model.Truncate(a.Description, 80), State: st})
			drawn[id] = true
		}
	}

	if len(repoOrder) > 0 {
		g.clusters = append(g.clusters, toolkit.GraphCluster{ID: reposCluster, Label: repoClusterLabel})
		for _, k := range repoOrder {
			g.nodes = append(g.nodes, toolkit.GraphNode{ID: repoPrefix + k, Label: model.Truncate(repoName(k), 12), Cluster: reposCluster, Category: "repo"})
			drawn[repoPrefix+k] = true
		}
	}
	leaseEdge := map[string]bool{}
	for _, l := range s.Leases { // live first, so a live lease wins its edge
		from, to := sessionPrefix+model.ShortID(l.Owner), repoPrefix+l.Repo
		id := leasePrefix + model.ShortID(l.Owner) + ":" + l.Repo
		if !drawn[from] || !drawn[to] || leaseEdge[id] {
			continue
		}
		leaseEdge[id] = true
		st := toolkit.GraphEdgeNormal
		if l.Expired {
			st = toolkit.GraphEdgeIdle
		}
		g.edges = append(g.edges, toolkit.GraphEdge{ID: id, From: from, To: to, Label: model.Truncate(l.Note, 80), State: st})
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
		g.edges = append(g.edges, toolkit.GraphEdge{ID: tellPrefix + strconv.Itoa(i), From: from, To: to, Label: model.Truncate(t.Text, 80), Weight: 100})
	}
	return g
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

// repoName is the repository half of org/repo.
func repoName(key string) string {
	if i := strings.LastIndex(key, "/"); i >= 0 {
		return key[i+1:]
	}
	return key
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
