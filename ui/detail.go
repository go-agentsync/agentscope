package ui

import (
	"strconv"
	"strings"

	"github.com/go-agentsync/agentscope/model"
)

// maxDetailLines bounds the detail pane; what is cut is said.
const maxDetailLines = 200

// refreshDetail rewrites the detail pane for the current selection.
func (vm *ViewModel) refreshDetail() {
	var lines []string
	var gauges []CtxRow
	if vm.have {
		lines = detailLines(vm.snap, vm.Graph.Selected.Get())
		gauges = contextRows(vm.snap, vm.Graph.Selected.Get())
	}
	setList(vm.Contexts, gauges)
	setList(vm.Detail, capLines(lines))
}

// capLines bounds a detail pane to maxDetailLines raw lines and says what was
// cut. Breaking a long line to the pane's width is the widget's job (a WrapList).
func capLines(lines []string) []string {
	if len(lines) > maxDetailLines {
		more := len(lines) - maxDetailLines
		lines = append(lines[:maxDetailLines:maxDetailLines], Tf("d.moreLines", more))
	}
	return lines
}

// gaugeName is the name column of a gauge, cut to fit it.
func gaugeName(s string) string { return model.Truncate(s, 26) }

// contextRows are the gauges of a selection: a session's own and each of its
// sub-agents', or one sub-agent's. Anything else has none.
func contextRows(s model.Snapshot, sel string) []CtxRow {
	w := windowOf(s)
	switch {
	case strings.HasPrefix(sel, "node:"+sessionPrefix):
		short := strings.TrimPrefix(sel, "node:"+sessionPrefix)
		se, ok := s.SessionByShort(short)
		if !ok {
			return nil
		}
		rows := []CtxRow{ctxOf(Tf("d.session", short), se.Context, w)}
		for _, a := range se.Agents {
			rows = append(rows, ctxOf(gaugeName(agentLabel(a)), a.Context, s.WindowOfAgent(a)))
		}
		return rows
	case strings.HasPrefix(sel, "node:"+agentPrefix):
		short, id, _ := strings.Cut(strings.TrimPrefix(sel, "node:"+agentPrefix), ":")
		se, _ := s.SessionByShort(short)
		for _, a := range se.Agents {
			if a.ID == id {
				return []CtxRow{ctxOf(gaugeName(agentLabel(a)), a.Context, s.WindowOfAgent(a))}
			}
		}
	}
	return nil
}

// agentLabel names a sub-agent in a gauge: its type, or "agent", and its id.
func agentLabel(a model.Agent) string {
	t := a.Type
	if t == "" {
		t = T("d.agent")
	}
	return t + " " + a.ID
}

// detailLines describes what is selected ("node:<id>" or "edge:<id>"), or the
// anomalies when nothing is.
func detailLines(s model.Snapshot, sel string) []string {
	switch {
	case strings.HasPrefix(sel, "node:"+sessionPrefix):
		return sessionDetail(s, strings.TrimPrefix(sel, "node:"+sessionPrefix))
	case strings.HasPrefix(sel, "node:"+agentPrefix):
		return agentDetail(s, strings.TrimPrefix(sel, "node:"+agentPrefix))
	case strings.HasPrefix(sel, "node:"+repoPrefix):
		return repoDetail(s, strings.TrimPrefix(sel, "node:"+repoPrefix))
	case strings.HasPrefix(sel, "node:"+orgPrefix):
		return orgDetail(s, strings.TrimPrefix(sel, "node:"+orgPrefix))
	case strings.HasPrefix(sel, "node:"+morePrefix):
		short := strings.TrimPrefix(sel, "node:"+morePrefix)
		se, _ := s.SessionByShort(short)
		return []string{Tf("d.moreHead", short), Tf("d.moreCount", len(se.Agents), MaxGraphAgents), T("d.moreHint")}
	case strings.HasPrefix(sel, "edge:"):
		return edgeDetail(s, strings.TrimPrefix(sel, "edge:"))
	}
	return anomalyLines(s)
}

// modelLines is the model of a session or sub-agent: the latest, with its raw
// id, and, when more than one was seen, every model newest first. A missing or
// unrecognised id is shown as "?" with the raw id; nothing is guessed.
func modelLines(s model.Snapshot, latest string, seen []model.ModelSeen) []string {
	lines := []string{Tf("d.model", model.ModelLabel(latest))}
	if len(seen) > 1 {
		lines = append(lines, T("d.modelsSeen"))
		for _, m := range seen {
			lines = append(lines, Tf("d.modelSeen", model.ModelLabel(m.ID), ago(s.At, m.Last)))
		}
	}
	return lines
}

func anomalyLines(s model.Snapshot) []string {
	lines := []string{Tf("d.anom", len(s.Anomalies))}
	for _, a := range s.Anomalies {
		l := a.Kind + ": " + a.Subject
		if a.Detail != "" {
			l += " - " + a.Detail
		}
		lines = append(lines, l)
	}
	st := s.Stats
	lines = append(lines,
		"",
		// Only what holds still between passes: a per-pass byte count or timing here
		// would repaint the panel on every snapshot of an idle network.
		Tf("d.read", st.TranscriptsSeen, st.LeasesRead, st.LeasesTorn, st.TellsRead, st.TellsDropped),
		Tf("d.expiredNot", st.ExpiredNotListed))
	return lines
}

func sessionDetail(s model.Snapshot, short string) []string {
	se, ok := s.SessionByShort(short)
	if !ok {
		return []string{Tf("d.sessNotIn", short)}
	}
	state := T("d.stIdle")
	if se.Active {
		state = Tf("d.stActive", s.ActiveFor)
	}
	if !se.HasTranscript {
		state = T("d.stNoTr")
	}
	lines := []string{
		Tf("d.session", se.ID),
		Tf("d.state", state, ago(s.At, se.LastActivity)),
		Tf("d.task", s.Task(se)),
	}
	add := func(k, v string) {
		if v != "" {
			lines = append(lines, Tf("d.kv", T(k), v))
		}
	}
	lines = append(lines, modelLines(s, se.Model, se.Models)...)
	lines = append(lines, Tf("d.context", contextText(se.Context, windowOf(s))))
	add("d.k.title", se.Title)
	add("d.k.slug", se.Slug)
	add("d.k.cwd", se.Cwd)
	add("d.k.cwdrepo", se.CwdRepo)
	add("d.k.branch", se.Branch)
	add("d.k.pr", se.PR)
	add("d.k.project", se.Project)
	lines = append(lines, Tf("d.sent", se.Sent, se.MemAdds))
	ls := s.LeasesOf(se.ID)
	lines = append(lines, Tf("d.leases", len(ls)))
	for _, l := range ls {
		lines = append(lines, "  "+l.Repo+" ["+leaseState(s, l)+"] "+l.Note)
	}
	lines = append(lines, Tf("d.subAgents", len(se.Agents)))
	for _, a := range se.Agents {
		act := ago(s.At, a.LastActivity)
		if a.Active {
			act = T("d.active")
		}
		lines = append(lines, "  "+a.Type+" ["+act+", "+modelShort(a.Model)+"] "+a.Description)
	}
	for _, t := range s.Tells {
		switch {
		case model.ShortID(t.From) == short:
			lines = append(lines, Tf("d.tellSent", t.Target, t.Text))
		case t.TargetKind == model.TargetSession && t.Target == short:
			lines = append(lines, Tf("d.tellFrom", model.ShortID(t.From), t.Text))
		}
	}
	return lines
}

// leaseState says until when a lease is held, or since when it has expired.
func leaseState(s model.Snapshot, l model.Lease) string {
	if l.Expired {
		return Tf("d.expiredAgo", ago(s.At, l.Expires))
	}
	return Tf("d.heldUntil", l.Expires.Format("15:04"))
}

func agentDetail(s model.Snapshot, rest string) []string {
	short, id, _ := strings.Cut(rest, ":")
	se, _ := s.SessionByShort(short)
	for _, a := range se.Agents {
		if a.ID != id {
			continue
		}
		act := Tf("d.idleLast", ago(s.At, a.LastActivity))
		if a.Active {
			act = T("d.active")
		}
		lines := []string{
			Tf("d.subAgent", a.ID), Tf("d.type", a.Type), Tf("d.stateOnly", act),
			Tf("d.depth", a.Depth), Tf("d.spawnedBy", short),
			Tf("d.desc", a.Description), Tf("d.toolUse", a.ToolUseID),
		}
		lines = append(lines, Tf("d.context", contextText(a.Context, s.WindowOfAgent(a))))
		return append(lines, modelLines(s, a.Model, a.Models)...)
	}
	return []string{Tf("d.agentNotIn", id)}
}

// orgDetail lists the repositories nobody holds under one org, in full.
func orgDetail(s model.Snapshot, org string) []string {
	// Held is what the graph calls held: a live lease of a listed session.
	listed := map[string]bool{}
	for _, se := range s.Sessions {
		listed[se.ID] = true
	}
	held := map[string]bool{}
	for _, l := range s.Leases {
		if !l.Expired && listed[l.Owner] {
			held[l.Repo] = true
		}
	}
	var repos []string
	seen := map[string]bool{}
	for _, l := range s.Leases {
		if orgOf(l.Repo) == org && !held[l.Repo] && !seen[l.Repo] {
			seen[l.Repo] = true
			repos = append(repos, l.Repo)
		}
	}
	lines := []string{Tf("d.orgHead", org, len(repos)), T("d.orgHint")}
	return append(lines, repos...)
}

func repoDetail(s model.Snapshot, key string) []string {
	lines := []string{Tf("d.repo", key)}
	n := 0
	for _, l := range s.Leases {
		if l.Repo != key {
			continue
		}
		if n == 0 {
			lines = append(lines, Tf("d.kind", string(l.Kind)))
			if l.Path != "" {
				lines = append(lines, Tf("d.clone", l.Path))
			}
		}
		n++
		lines = append(lines, Tf("d.leaseBy", model.ShortID(l.Owner), leaseState(s, l), l.Note))
	}
	for _, t := range s.Tells {
		if t.TargetKind == model.TargetResource && t.Target == key {
			lines = append(lines, Tf("d.tellFrom", model.ShortID(t.From), t.Text))
		}
	}
	return lines
}

func edgeDetail(s model.Snapshot, id string) []string {
	switch {
	case strings.HasPrefix(id, tellPrefix):
		i, err := strconv.Atoi(strings.TrimPrefix(id, tellPrefix))
		if err != nil || i < 0 || i >= len(s.Tells) {
			return []string{T("d.tellNotIn")}
		}
		t := s.Tells[i]
		return []string{Tf("d.tellEdge", model.ShortID(t.From), t.Target, string(t.TargetKind)), Tf("d.at", t.At.Format("2006-01-02 15:04:05")), t.Text}
	case strings.HasPrefix(id, leasePrefix):
		owner, key, _ := strings.Cut(strings.TrimPrefix(id, leasePrefix), ":")
		for _, l := range s.Leases {
			if model.ShortID(l.Owner) == owner && l.Repo == key {
				return []string{Tf("d.leaseEdge", key, owner), Tf("d.acquired", l.Acquired.Format("2006-01-02 15:04")), Tf("d.expires", l.Expires.Format("2006-01-02 15:04")), Tf("d.note", l.Note)}
			}
		}
		return []string{T("d.leaseNotIn")}
	case strings.HasPrefix(id, spawnPrefix):
		return agentDetail(s, strings.TrimPrefix(id, spawnPrefix+agentPrefix))
	}
	return []string{Tf("d.edge", id)}
}
