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
	lines = wrap(lines, max(vm.DetailChars.Get(), minDetailChars))
	if len(lines) > maxDetailLines {
		more := len(lines) - maxDetailLines
		lines = append(lines[:maxDetailLines], "... "+strconv.Itoa(more)+" more line(s)")
	}
	setList(vm.Detail, lines)
}

// detailWidth is the width, in characters, the detail pane breaks lines at until
// the Scene has measured its panel; minDetailChars is the narrowest it is told.
const (
	detailWidth    = 84
	minDetailChars = 16
)

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
		rows := []CtxRow{ctxOf("session "+short, se.Context, w)}
		for _, a := range se.Agents {
			rows = append(rows, ctxOf(gaugeName(agentLabel(a)), a.Context, w))
		}
		return rows
	case strings.HasPrefix(sel, "node:"+agentPrefix):
		short, id, _ := strings.Cut(strings.TrimPrefix(sel, "node:"+agentPrefix), ":")
		se, _ := s.SessionByShort(short)
		for _, a := range se.Agents {
			if a.ID == id {
				return []CtxRow{ctxOf(gaugeName(agentLabel(a)), a.Context, w)}
			}
		}
	}
	return nil
}

// agentLabel names a sub-agent in a gauge: its type, or "agent", and its id.
func agentLabel(a model.Agent) string {
	t := a.Type
	if t == "" {
		t = "agent"
	}
	return t + " " + a.ID
}

// wrap breaks every line longer than width runes into continuation lines
// indented by two spaces; a ListBox clips, it does not wrap.
func wrap(lines []string, width int) []string {
	var out []string
	for _, l := range lines {
		r := []rune(l)
		first := true
		for len(r) > width {
			cut := width
			for i := width; i > width/2; i-- {
				if r[i] == ' ' {
					cut = i
					break
				}
			}
			out = append(out, indent(first)+strings.TrimRight(string(r[:cut]), " "))
			r = []rune(strings.TrimLeft(string(r[cut:]), " "))
			first = false
		}
		out = append(out, indent(first)+string(r))
	}
	return out
}

func indent(first bool) string {
	if first {
		return ""
	}
	return "  "
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
		return []string{"sub-agents of " + short + " not drawn", "listed " + strconv.Itoa(len(se.Agents)) + ", drawn " + strconv.Itoa(MaxGraphAgents), "(select the session for the newest)"}
	case strings.HasPrefix(sel, "edge:"):
		return edgeDetail(s, strings.TrimPrefix(sel, "edge:"))
	}
	return anomalyLines(s)
}

// modelLines is the model of a session or sub-agent: the latest, with its raw
// id, and, when more than one was seen, every model newest first. A missing or
// unrecognised id is shown as "?" with the raw id; nothing is guessed.
func modelLines(s model.Snapshot, latest string, seen []model.ModelSeen) []string {
	lines := []string{"model: " + model.ModelLabel(latest)}
	if len(seen) > 1 {
		lines = append(lines, "models seen (newest first):")
		for _, m := range seen {
			lines = append(lines, "  "+model.ModelLabel(m.ID)+", last "+ago(s.At, m.Last)+" ago")
		}
	}
	return lines
}

func anomalyLines(s model.Snapshot) []string {
	lines := []string{"ANOMALIES (" + strconv.Itoa(len(s.Anomalies)) + "): shown, not hidden"}
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
		"read: "+strconv.Itoa(st.TranscriptsRead)+" of "+strconv.Itoa(st.TranscriptsSeen)+" transcripts this pass ("+strconv.FormatInt(st.BytesRead/1024, 10)+" KiB), "+
			strconv.Itoa(st.LeasesRead)+" leases ("+strconv.Itoa(st.LeasesTorn)+" torn), "+strconv.Itoa(st.TellsRead)+" tells ("+strconv.Itoa(st.TellsDropped)+" outside the window)",
		"expired leases beyond the horizon, not listed: "+strconv.Itoa(st.ExpiredNotListed),
		"collected in "+strconv.FormatInt(st.ElapsedMicros/1000, 10)+" ms")
	return lines
}

func sessionDetail(s model.Snapshot, short string) []string {
	se, ok := s.SessionByShort(short)
	if !ok {
		return []string{"session " + short + ": not in this snapshot"}
	}
	state := "idle"
	if se.Active {
		state = "ACTIVE (written within " + s.ActiveFor + ")"
	}
	if !se.HasTranscript {
		state = "no transcript found"
	}
	lines := []string{
		"session " + se.ID,
		"state: " + state + ", last activity " + ago(s.At, se.LastActivity) + " ago",
		"task: " + s.Task(se),
	}
	add := func(k, v string) {
		if v != "" {
			lines = append(lines, k+": "+v)
		}
	}
	lines = append(lines, modelLines(s, se.Model, se.Models)...)
	lines = append(lines, "context: "+contextText(se.Context, windowOf(s)))
	add("title", se.Title)
	add("slug", se.Slug)
	add("cwd", se.Cwd)
	add("cwd repo", se.CwdRepo)
	add("branch", se.Branch)
	add("pull request", se.PR)
	add("project", se.Project)
	lines = append(lines, "sent "+strconv.Itoa(se.Sent)+" SendMessage call(s) in the observed window; "+strconv.Itoa(se.MemAdds)+" memory entries recorded")
	ls := s.LeasesOf(se.ID)
	lines = append(lines, "leases ("+strconv.Itoa(len(ls))+"):")
	for _, l := range ls {
		st := "held until " + l.Expires.Format("15:04")
		if l.Expired {
			st = "expired " + ago(s.At, l.Expires) + " ago"
		}
		lines = append(lines, "  "+l.Repo+" ["+st+"] "+l.Note)
	}
	lines = append(lines, "sub-agents ("+strconv.Itoa(len(se.Agents))+"):")
	for _, a := range se.Agents {
		act := ago(s.At, a.LastActivity)
		if a.Active {
			act = "ACTIVE"
		}
		lines = append(lines, "  "+a.Type+" ["+act+", "+modelShort(a.Model)+"] "+a.Description)
	}
	for _, t := range s.Tells {
		switch {
		case model.ShortID(t.From) == short:
			lines = append(lines, "tell sent to "+t.Target+": "+t.Text)
		case t.TargetKind == model.TargetSession && t.Target == short:
			lines = append(lines, "tell from "+model.ShortID(t.From)+": "+t.Text)
		}
	}
	return lines
}

func agentDetail(s model.Snapshot, rest string) []string {
	short, id, _ := strings.Cut(rest, ":")
	se, _ := s.SessionByShort(short)
	for _, a := range se.Agents {
		if a.ID != id {
			continue
		}
		act := "idle, last " + ago(s.At, a.LastActivity) + " ago"
		if a.Active {
			act = "ACTIVE"
		}
		lines := []string{
			"sub-agent " + a.ID, "type: " + a.Type, "state: " + act,
			"spawn depth: " + strconv.Itoa(a.Depth), "spawned by session " + short,
			"description: " + a.Description, "tool use: " + a.ToolUseID,
		}
		lines = append(lines, "context: "+contextText(a.Context, windowOf(s)))
		return append(lines, modelLines(s, a.Model, a.Models)...)
	}
	return []string{"sub-agent " + id + ": not in this snapshot"}
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
	lines := []string{"org " + org + ": " + strconv.Itoa(len(repos)) + " repositories nobody holds", "(Enter or a second click expands it; Escape collapses)"}
	return append(lines, repos...)
}

func repoDetail(s model.Snapshot, key string) []string {
	lines := []string{"repository " + key}
	n := 0
	for _, l := range s.Leases {
		if l.Repo != key {
			continue
		}
		if n == 0 {
			lines = append(lines, "kind: "+string(l.Kind))
			if l.Path != "" {
				lines = append(lines, "clone: "+l.Path)
			}
		}
		n++
		st := "held until " + l.Expires.Format("15:04")
		if l.Expired {
			st = "expired " + ago(s.At, l.Expires) + " ago"
		}
		lines = append(lines, "lease by "+model.ShortID(l.Owner)+" ["+st+"] "+l.Note)
	}
	for _, t := range s.Tells {
		if t.TargetKind == model.TargetResource && t.Target == key {
			lines = append(lines, "tell from "+model.ShortID(t.From)+": "+t.Text)
		}
	}
	return lines
}

func edgeDetail(s model.Snapshot, id string) []string {
	switch {
	case strings.HasPrefix(id, tellPrefix):
		i, err := strconv.Atoi(strings.TrimPrefix(id, tellPrefix))
		if err != nil || i < 0 || i >= len(s.Tells) {
			return []string{"tell: not in this snapshot"}
		}
		t := s.Tells[i]
		return []string{"tell " + model.ShortID(t.From) + " -> " + t.Target + " (" + string(t.TargetKind) + ")", "at " + t.At.Format("2006-01-02 15:04:05"), t.Text}
	case strings.HasPrefix(id, leasePrefix):
		owner, key, _ := strings.Cut(strings.TrimPrefix(id, leasePrefix), ":")
		for _, l := range s.Leases {
			if model.ShortID(l.Owner) == owner && l.Repo == key {
				return []string{"lease " + key + " held by " + owner, "acquired " + l.Acquired.Format("2006-01-02 15:04"), "expires " + l.Expires.Format("2006-01-02 15:04"), "note: " + l.Note}
			}
		}
		return []string{"lease: not in this snapshot"}
	case strings.HasPrefix(id, spawnPrefix):
		return agentDetail(s, strings.TrimPrefix(id, spawnPrefix+agentPrefix))
	}
	return []string{"edge " + id}
}
