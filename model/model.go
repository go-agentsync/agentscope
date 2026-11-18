// Package model is the wire and screen model of agentscope: one Snapshot is
// everything the UI shows, and it carries METADATA only -- ids, working
// directories resolved to repositories, tool names, agent descriptions, lease
// notes, tell texts and timestamps. Nothing here ever holds a prompt or a
// response; the collector never reads those fields (see package collect).
package model

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

// Version is the snapshot schema version; the browser refuses a newer one.
const Version = 1

// Snapshot is the whole picture at one instant.
type Snapshot struct {
	Version   int       `json:"version"`
	At        time.Time `json:"at"`
	ActiveFor string    `json:"activeFor"` // the activity window, e.g. "10m0s"
	Horizon   string    `json:"horizon"`   // sessions older than this are not listed
	Sessions  []Session `json:"sessions"`
	Leases    []Lease   `json:"leases"`
	Tells     []Tell    `json:"tells"`
	Anomalies []Anomaly `json:"anomalies"`
	Stats     Stats     `json:"stats"`
}

// Session is one Claude Code session, with its sub-agents.
type Session struct {
	ID            string    `json:"id"`
	Short         string    `json:"short"` // first 8 characters of ID
	Title         string    `json:"title,omitempty"`
	Slug          string    `json:"slug,omitempty"`
	Cwd           string    `json:"cwd,omitempty"`
	CwdRepo       string    `json:"cwdRepo,omitempty"` // Cwd resolved to org/repo, when it is one
	Branch        string    `json:"branch,omitempty"`
	PR            string    `json:"pr,omitempty"`
	Project       string    `json:"project,omitempty"`
	LastActivity  time.Time `json:"lastActivity"`
	Active        bool      `json:"active"`
	HasTranscript bool      `json:"hasTranscript"`
	Sent          int       `json:"sent"`    // SendMessage calls seen in the observed window
	MemAdds       int       `json:"memAdds"` // entries in the agentsync session record
	Agents        []Agent   `json:"agents,omitempty"`
	// Model is the id of the latest assistant entry's model ("" when none was
	// seen); Models is every model seen in the observed window, newest first.
	Model  string      `json:"model,omitempty"`
	Models []ModelSeen `json:"models,omitempty"`
}

// ModelSeen is a model id and when it was last seen answering.
type ModelSeen struct {
	ID   string    `json:"id"`
	Last time.Time `json:"last"`
}

var familyRE = regexp.MustCompile(`^claude-(opus|sonnet|haiku|fable)-(\d+)(?:-(\d{1,2}))?(?:-\d{8})?(?:\[[^\]]*\])?$`)

// ModelName is the short family name of a model id ("Opus 5.5") and whether
// the id was understood. It never guesses: a missing or unrecognised id is "?"
// and the caller shows the raw id beside it.
func ModelName(id string) (string, bool) {
	m := familyRE.FindStringSubmatch(id)
	if m == nil {
		return "?", false
	}
	fam := strings.ToUpper(m[1][:1]) + m[1][1:]
	if m[3] == "" {
		return fam + " " + m[2], true
	}
	return fam + " " + m[2] + "." + m[3], true
}

// ModelFamily is the family alone ("Opus"), or "?" for an unknown id.
func ModelFamily(id string) string {
	name, ok := ModelName(id)
	if !ok {
		return "?"
	}
	fam, _, _ := strings.Cut(name, " ")
	return fam
}

// ModelLabel is "Opus 5.5 (claude-opus-5-5)", or "? (<raw id>)" for an unknown
// id, or "? (none seen)" when there is none.
func ModelLabel(id string) string {
	name, _ := ModelName(id)
	if id == "" {
		return "? (none seen)"
	}
	return name + " (" + id + ")"
}

// Agent is one sub-agent of a session.
type Agent struct {
	Model        string      `json:"model,omitempty"`
	Models       []ModelSeen `json:"models,omitempty"`
	ID           string      `json:"id"`
	Type         string      `json:"type,omitempty"`
	Description  string      `json:"description,omitempty"`
	ToolUseID    string      `json:"toolUseId,omitempty"`
	Depth        int         `json:"depth"`
	LastActivity time.Time   `json:"lastActivity"`
	Active       bool        `json:"active"`
}

// RepoKind says how a lease resource was understood.
type RepoKind string

const (
	// KindRepo is an org/repo that matches a clone on this machine.
	KindRepo RepoKind = "repo"
	// KindRepoForm is an org/repo spelling with no clone here.
	KindRepoForm RepoKind = "repo-form"
	// KindOrg is an organisation that has clones here.
	KindOrg RepoKind = "org"
	// KindUnresolved is a key that is no repository: a note used as the key,
	// a bare name, a flag. It is shown, flagged, never hidden.
	KindUnresolved RepoKind = "unresolved"
)

// Lease is one advisory lease, expired ones included.
type Lease struct {
	Resource string    `json:"resource"` // as written in the lease
	Repo     string    `json:"repo"`     // normalised key (org/repo when resolved)
	Kind     RepoKind  `json:"kind"`
	Path     string    `json:"path,omitempty"` // the clone, when there is one
	Owner    string    `json:"owner"`
	PID      int       `json:"pid"`
	Note     string    `json:"note,omitempty"`
	Acquired time.Time `json:"acquired"`
	Expires  time.Time `json:"expires"`
	Expired  bool      `json:"expired"`
}

// TargetKind says what a tell was addressed to.
type TargetKind string

const (
	TargetSession  TargetKind = "session"
	TargetResource TargetKind = "resource"
)

// Tell is one `agentsync tell` message.
type Tell struct {
	Target     string     `json:"target"` // the session short id or the repo key
	TargetKind TargetKind `json:"targetKind"`
	From       string     `json:"from"` // sender session id
	Text       string     `json:"text"` // truncated
	At         time.Time  `json:"at"`
}

// Anomaly is something odd the collector met and chose to show.
type Anomaly struct {
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
	Detail  string `json:"detail,omitempty"`
}

// Anomaly kinds.
const (
	AnomalyUnresolvedKey   = "unresolved key"
	AnomalyOrphanLease     = "lease owner has no transcript"
	AnomalyUnknownSender   = "tell from unknown session"
	AnomalyUnknownTarget   = "tell to unknown session"
	AnomalyBadLine         = "unparsable line"
	AnomalyOversizedLine   = "oversized line skipped"
	AnomalyUnreadable      = "unreadable file"
	AnomalyTranscriptStall = "transcript shrank"
)

// Stats counts what the collector looked at, so a zero is distinguishable
// from "could not read".
type Stats struct {
	TranscriptsSeen  int   `json:"transcriptsSeen"`
	TranscriptsRead  int   `json:"transcriptsRead"`
	BytesRead        int64 `json:"bytesRead"`
	LeasesRead       int   `json:"leasesRead"`
	LeasesTorn       int   `json:"leasesTorn"`
	TellsRead        int   `json:"tellsRead"`
	TellsDropped     int   `json:"tellsDropped"`
	SubAgentsRead    int   `json:"subAgentsRead"`
	SessionRecords   int   `json:"sessionRecords"`
	ElapsedMicros    int64 `json:"elapsedMicros"`
	LinesSkipped     int   `json:"linesSkipped"`
	LinesUnparsable  int   `json:"linesUnparsable"`
	ReadErrors       int   `json:"readErrors"`
	ExpiredNotListed int   `json:"expiredNotListed"`
}

// ModelCount is how many sessions and sub-agents run one model family.
type ModelCount struct {
	Family string
	N      int
}

// ModelCounts counts the listed sessions and sub-agents by model family, most
// first (ties by name). "?" counts those whose model is unknown or unseen.
func (s Snapshot) ModelCounts() []ModelCount {
	n := map[string]int{}
	for _, se := range s.Sessions {
		n[ModelFamily(se.Model)]++
		for _, a := range se.Agents {
			n[ModelFamily(a.Model)]++
		}
	}
	out := make([]ModelCount, 0, len(n))
	for f, c := range n {
		out = append(out, ModelCount{f, c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].N != out[j].N {
			return out[i].N > out[j].N
		}
		return out[i].Family < out[j].Family
	})
	return out
}

// Counts is the header line of the UI.
type Counts struct {
	Sessions, Active, Agents, ActiveAgents, Repos, Held, Leases, Tells, Anomalies int
}

// Counts summarises the snapshot.
func (s Snapshot) Counts() Counts {
	c := Counts{Sessions: len(s.Sessions), Leases: len(s.Leases), Tells: len(s.Tells), Anomalies: len(s.Anomalies)}
	repos := map[string]bool{}
	for _, se := range s.Sessions {
		if se.Active {
			c.Active++
		}
		c.Agents += len(se.Agents)
		for _, a := range se.Agents {
			if a.Active {
				c.ActiveAgents++
			}
		}
	}
	for _, l := range s.Leases {
		repos[l.Repo] = true
		if !l.Expired {
			c.Held++
		}
	}
	c.Repos = len(repos)
	return c
}

// SessionByShort finds a session by its short id.
func (s Snapshot) SessionByShort(short string) (Session, bool) {
	for _, se := range s.Sessions {
		if se.Short == short {
			return se, true
		}
	}
	return Session{}, false
}

// LeasesOf returns the leases a session holds or held, live first and then by
// most recent acquisition.
func (s Snapshot) LeasesOf(id string) []Lease {
	var out []Lease
	for _, l := range s.Leases {
		if l.Owner == id {
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Expired != out[j].Expired {
			return !out[i].Expired
		}
		return out[i].Acquired.After(out[j].Acquired)
	})
	return out
}

// Task is the best one-line statement of what a session is doing: the note of
// its live lease, else its title, else its slug.
func (s Snapshot) Task(se Session) string {
	for _, l := range s.LeasesOf(se.ID) {
		if !l.Expired && l.Note != "" {
			return l.Note
		}
	}
	switch {
	case se.Title != "":
		return se.Title
	case se.Slug != "":
		return se.Slug
	}
	for _, l := range s.LeasesOf(se.ID) {
		if l.Note != "" {
			return l.Note
		}
	}
	return ""
}

// ShortID is the first 8 characters of an id.
func ShortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// Truncate cuts s to at most n runes, with an ellipsis, on a rune boundary.
func Truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
