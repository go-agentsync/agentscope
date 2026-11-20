// Package collect reads the agent network off this machine, read-only:
//
//   - leases       ~/.claude/agentsync/locks/*.lease
//   - tells        ~/.claude/agentsync/locks/<target>.messages
//   - session recs ~/.claude/agentsync/sessions/<uuid>.json
//   - transcripts  ~/.claude/projects/<project>/<session>.jsonl, and the
//     sub-agents under <project>/<session>/subagents/
//
// and folds them into one model.Snapshot. It never writes to any of them, it
// reads transcripts incrementally and bounded, and it extracts metadata only
// (see transcript.go for what is and is not decoded).
package collect

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-agentsync/agentsync/lease"
)

// Options configures a Collector. The zero value is not useful; use
// DefaultOptions.
type Options struct {
	ClaudeDir     string        // ~/.claude
	LocalhostRoot string        // ~/Documents/VCS/GIT/localhost
	GithubRoot    string        // ~/Documents/VCS/GIT/github.com
	Active        time.Duration // a session is active when written within this
	Horizon       time.Duration // sessions and expired leases older than this are not listed
	TellWindow    time.Duration // tells older than this are not listed
	TailBytes     int64         // first read of a transcript covers its last TailBytes
	MaxLine       int           // a transcript line longer than this is skipped
	MaxTellText   int           // runes of a tell text kept
	MaxAgents     int           // sub-agents listed per session (newest first)
	Now           func() time.Time
}

// DefaultOptions are the settings of `agentscope serve` without flags.
func DefaultOptions() (Options, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Options{}, err
	}
	return Options{
		ClaudeDir:     filepath.Join(home, ".claude"),
		LocalhostRoot: filepath.Join(home, "Documents", "VCS", "GIT", "localhost"),
		GithubRoot:    filepath.Join(home, "Documents", "VCS", "GIT", "github.com"),
		Active:        10 * time.Minute,
		Horizon:       12 * time.Hour,
		TellWindow:    24 * time.Hour,
		TailBytes:     4 << 20,
		MaxLine:       1 << 20,
		MaxTellText:   240,
		MaxAgents:     40,
	}, nil
}

// Collector holds what is remembered between snapshots: the offset reached in
// each transcript and the parsed form of each small file that has not changed.
type Collector struct {
	opt Options
	res *Resolver

	mu     sync.Mutex
	trans  map[string]*tstate // by transcript path
	leases map[string]cachedLease
	tells  map[string]cachedTells
	recs   map[string]cachedRec
	metas  map[string]cachedMeta
}

type fileKey struct {
	mod  time.Time
	size int64
}

type cachedLease struct {
	fileKey
	l  lease.Lease
	ok bool
}

type cachedTells struct {
	fileKey
	msgs    []lease.Message
	dropped int
}

type cachedRec struct {
	fileKey
	n    int
	last time.Time
}

type cachedMeta struct {
	fileKey
	m  agentMeta
	ok bool
}

type agentMeta struct {
	AgentType   string `json:"agentType"`
	Description string `json:"description"`
	ToolUseID   string `json:"toolUseId"`
	SpawnDepth  int    `json:"spawnDepth"`
}

// New returns a Collector.
func New(o Options) *Collector {
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Collector{
		opt:   o,
		res:   &Resolver{LocalhostRoot: o.LocalhostRoot, GithubRoot: o.GithubRoot, Now: o.Now},
		trans: map[string]*tstate{}, leases: map[string]cachedLease{},
		tells: map[string]cachedTells{}, recs: map[string]cachedRec{}, metas: map[string]cachedMeta{},
	}
}

// maxSmallFile bounds every file that is read whole.
const maxSmallFile = 8 << 20

// readSmall reads a whole file of at most max bytes.
func readSmall(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errors.New("file larger than the read bound")
	}
	return b, nil
}

type transFile struct {
	path    string
	project string
	mod     time.Time
	size    int64
}

// isUUID reports whether s has the shape of a session id.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if c != '-' {
				return false
			}
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}

func isShort(s string) bool {
	if len(s) != 8 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Snapshot collects the picture now.
func (c *Collector) Snapshot() model.Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	t0 := time.Now()
	now := c.opt.Now()
	snap := model.Snapshot{
		Version: model.Version, At: now,
		ActiveFor: c.opt.Active.String(), Horizon: c.opt.Horizon.String(),
	}
	snap.ContextWindow = CompactWindow(c.opt.ClaudeDir, osGetenv)
	st := &snap.Stats
	anom := map[string]model.Anomaly{}
	addAnom := func(kind, subject, detail string) {
		k := kind + "\x00" + subject
		if _, ok := anom[k]; !ok {
			anom[k] = model.Anomaly{Kind: kind, Subject: subject, Detail: detail}
		}
	}

	// --- every transcript on disk, newest known per session id -------------
	files := map[string]transFile{}
	projDir := filepath.Join(c.opt.ClaudeDir, "projects")
	projects, err := os.ReadDir(projDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		st.ReadErrors++
		addAnom(model.AnomalyUnreadable, projDir, err.Error())
	}
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		ents, err := os.ReadDir(filepath.Join(projDir, p.Name()))
		if err != nil {
			st.ReadErrors++
			addAnom(model.AnomalyUnreadable, filepath.Join(projDir, p.Name()), err.Error())
			continue
		}
		for _, e := range ents {
			id, ok := strings.CutSuffix(e.Name(), ".jsonl")
			if !ok || !isUUID(id) {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			st.TranscriptsSeen++
			if old, dup := files[id]; dup && old.mod.After(info.ModTime()) {
				continue
			}
			files[id] = transFile{path: filepath.Join(projDir, p.Name(), e.Name()), project: p.Name(), mod: info.ModTime(), size: info.Size()}
		}
	}

	// --- leases -------------------------------------------------------------
	leases := c.readLeases(now, st, addAnom)
	for _, l := range leases { // lease keys teach the resolver where an org ends
		c.res.Resolve(l.Resource)
	}
	// --- tells --------------------------------------------------------------
	tells := c.readTells(now, st)
	// --- agentsync session records -----------------------------------------
	recs := c.readRecords(st)

	// --- which sessions are listed -----------------------------------------
	want := map[string]bool{}
	horizon := now.Add(-c.opt.Horizon)
	for id, f := range files {
		if f.mod.After(horizon) {
			want[id] = true
		}
	}
	for _, l := range leases {
		if !l.Expired(now) && l.Owner != "" {
			want[l.Owner] = true
		}
	}
	for id, r := range recs {
		if r.mod.After(horizon) {
			want[id] = true
		}
	}
	for _, t := range tells {
		if isUUID(t.From) {
			want[t.From] = true
		}
	}

	byShort := map[string]string{}
	for id := range want {
		byShort[model.ShortID(id)] = id
	}
	for id := range files {
		if _, ok := byShort[model.ShortID(id)]; !ok {
			byShort[model.ShortID(id)] = id
		}
	}

	ids := make([]string, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	active := now.Add(-c.opt.Active)
	for _, id := range ids {
		se := model.Session{ID: id, Short: model.ShortID(id)}
		if r, ok := recs[id]; ok {
			se.MemAdds = r.n
			se.LastActivity = r.last
		}
		if f, ok := files[id]; ok {
			se.HasTranscript = true
			se.Project = f.project
			se.LastActivity = f.mod
			se.Active = f.mod.After(active)
			ts := c.trans[f.path]
			if ts == nil {
				ts = &tstate{}
				c.trans[f.path] = ts
			}
			c.readTranscript(f, ts, c.opt.TailBytes, st, addAnom)
			se.Model, se.Models = modelsOf(ts, horizon)
			se.Context = ts.ctx
			se.Slug, se.Cwd, se.Branch, se.PR, se.Sent = ts.slug, ts.cwd, ts.branch, ts.pr, ts.sent
			se.Title = ts.custom
			if se.Title == "" {
				se.Title = ts.title
			}
			se.CwdRepo = c.res.RepoOfCwd(ts.cwd)
			se.Agents = c.readAgents(filepath.Dir(f.path), id, now, st, addAnom)
		}
		snap.Sessions = append(snap.Sessions, se)
	}
	sort.SliceStable(snap.Sessions, func(i, j int) bool {
		a, b := snap.Sessions[i], snap.Sessions[j]
		if !a.LastActivity.Equal(b.LastActivity) {
			return a.LastActivity.After(b.LastActivity)
		}
		return a.ID < b.ID
	})

	// --- leases listed, and the anomalies they raise -----------------------
	owners := map[string]bool{}
	for _, l := range leases {
		owners[l.Owner] = true
	}
	known := func(id string) bool { _, ok := files[id]; return ok || owners[id] }
	unresolved := map[string]bool{}
	historic := 0
	for _, l := range leases {
		ml := model.Lease{
			Resource: l.Resource, Owner: l.Owner, PID: l.PID, Note: model.Truncate(l.Note, 200),
			Acquired: l.Acquired, Expires: l.Expires, Expired: now.After(l.Expires),
		}
		ml.Repo, ml.Kind, ml.Path = c.res.Resolve(l.Resource)
		listed := !(ml.Expired && ml.Expires.Before(horizon))
		if ml.Kind == model.KindUnresolved && !unresolved[ml.Repo] {
			unresolved[ml.Repo] = true
			if listed {
				addAnom(model.AnomalyUnresolvedKey, model.Truncate(ml.Repo, 80), "lease resource is no repository; owner "+model.ShortID(l.Owner))
			} else {
				historic++
			}
		}
		if _, ok := files[l.Owner]; !ok && !ml.Expired {
			addAnom(model.AnomalyOrphanLease, model.ShortID(l.Owner), "holds "+model.Truncate(ml.Repo, 60))
		}
		if !listed {
			st.ExpiredNotListed++
			continue
		}
		snap.Leases = append(snap.Leases, ml)
	}
	if historic > 0 {
		addAnom(model.AnomalyUnresolvedKey, "(expired before the horizon)",
			itoa(historic)+" lease key(s), all expired, are no repository; --horizon lists them")
	}
	sort.SliceStable(snap.Leases, func(i, j int) bool {
		a, b := snap.Leases[i], snap.Leases[j]
		if a.Expired != b.Expired {
			return !a.Expired
		}
		if !a.Acquired.Equal(b.Acquired) {
			return a.Acquired.After(b.Acquired)
		}
		return a.Resource < b.Resource
	})

	// --- tells --------------------------------------------------------------
	for _, t := range tells {
		mt := model.Tell{From: t.From, Text: model.Truncate(t.Text, c.opt.MaxTellText), At: t.At}
		switch {
		case isShort(t.Resource) || isUUID(t.Resource):
			mt.TargetKind = model.TargetSession
			mt.Target = model.ShortID(t.Resource)
			if _, ok := byShort[mt.Target]; !ok {
				addAnom(model.AnomalyUnknownTarget, mt.Target, "tell addressed to a session with no transcript")
			}
		default:
			mt.TargetKind = model.TargetResource
			mt.Target, _, _ = c.res.Resolve(t.Resource)
		}
		if !known(t.From) {
			addAnom(model.AnomalyUnknownSender, model.ShortID(t.From), "tell from a session with no transcript or lease")
		}
		snap.Tells = append(snap.Tells, mt)
	}
	sort.SliceStable(snap.Tells, func(i, j int) bool { return snap.Tells[i].At.After(snap.Tells[j].At) })

	for k, ts := range c.trans {
		_ = k
		if ts.skipped > 0 || ts.bad > 0 {
			st.LinesSkipped += ts.skipped
			st.LinesUnparsable += ts.bad
		}
	}
	if st.LinesSkipped > 0 {
		addAnom(model.AnomalyOversizedLine, "transcripts", itoa(st.LinesSkipped)+" line(s) over the bound were skipped")
	}
	if st.LinesUnparsable > 0 {
		addAnom(model.AnomalyBadLine, "transcripts", itoa(st.LinesUnparsable)+" line(s) were not JSON")
	}

	for _, a := range anom {
		snap.Anomalies = append(snap.Anomalies, a)
	}
	sort.Slice(snap.Anomalies, func(i, j int) bool {
		a, b := snap.Anomalies[i], snap.Anomalies[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Subject < b.Subject
	})
	st.ElapsedMicros = time.Since(t0).Microseconds()
	return snap
}

func itoa(n int) string { return strconv.Itoa(n) }

// readTranscript advances one transcript's state.
func (c *Collector) readTranscript(f transFile, ts *tstate, tail int64, st *model.Stats, addAnom func(k, s, d string)) {
	if ts.size == f.size && ts.offset == f.size {
		return // nothing appended
	}
	fh, err := os.Open(f.path)
	if err != nil {
		st.ReadErrors++
		addAnom(model.AnomalyUnreadable, model.ShortID(strings.TrimSuffix(filepath.Base(f.path), ".jsonl")), err.Error())
		return
	}
	defer fh.Close()
	if ts.size > f.size {
		addAnom(model.AnomalyTranscriptStall, model.ShortID(strings.TrimSuffix(filepath.Base(f.path), ".jsonl")), "the file is smaller than at the last read; it was read again from its tail")
	}
	n, err := scan(fh, f.size, ts, tail, c.opt.MaxLine)
	st.TranscriptsRead++
	st.BytesRead += n
	if err != nil {
		st.ReadErrors++
		addAnom(model.AnomalyUnreadable, filepath.Base(f.path), err.Error())
	}
}

// readAgents lists the sub-agents of a session, newest first, from the
// meta.json files beside their transcripts.
func (c *Collector) readAgents(projDir, id string, now time.Time, st *model.Stats, addAnom func(k, s, d string)) []model.Agent {
	dir := filepath.Join(projDir, id, "subagents")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	horizon := now.Add(-c.opt.Horizon)
	active := now.Add(-c.opt.Active)
	var out []model.Agent
	for _, e := range ents {
		aid, ok := strings.CutSuffix(strings.TrimPrefix(e.Name(), "agent-"), ".meta.json")
		if !ok || !strings.HasPrefix(e.Name(), "agent-") || !validSegment(strings.ToLower(aid)) {
			continue
		}
		mod := time.Time{}
		if info, err := os.Stat(filepath.Join(dir, "agent-"+aid+".jsonl")); err == nil {
			mod = info.ModTime()
		} else if info, err := e.Info(); err == nil {
			mod = info.ModTime()
		}
		if mod.Before(horizon) {
			continue
		}
		meta, ok := c.meta(filepath.Join(dir, e.Name()), e, st)
		if !ok {
			continue
		}
		out = append(out, model.Agent{
			ID: aid, Type: model.Truncate(meta.AgentType, 60), Description: model.Truncate(meta.Description, 200),
			ToolUseID: model.Truncate(meta.ToolUseID, 80), Depth: meta.SpawnDepth,
			LastActivity: mod, Active: mod.After(active),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].LastActivity.Equal(out[j].LastActivity) {
			return out[i].LastActivity.After(out[j].LastActivity)
		}
		return out[i].ID < out[j].ID
	})
	if c.opt.MaxAgents > 0 && len(out) > c.opt.MaxAgents {
		out = out[:c.opt.MaxAgents]
	}
	// The model of each listed sub-agent, from the tail of its own transcript.
	for i := range out {
		p := filepath.Join(dir, "agent-"+out[i].ID+".jsonl")
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		ts := c.trans[p]
		if ts == nil {
			ts = &tstate{}
			c.trans[p] = ts
		}
		c.readTranscript(transFile{path: p, mod: info.ModTime(), size: info.Size()}, ts, agentTailBytes, st, addAnom)
		out[i].Model, out[i].Models = modelsOf(ts, horizon)
		out[i].Context = ts.ctx
	}
	return out
}

// agentTailBytes is how much of a sub-agent transcript is read the first time:
// its model is in its recent entries, and there are many sub-agents.
const agentTailBytes = 256 << 10

// modelsOf is the latest model of a transcript and every model seen since the
// horizon, newest first.
func modelsOf(ts *tstate, horizon time.Time) (string, []model.ModelSeen) {
	var all []model.ModelSeen
	for _, m := range ts.models {
		if m.last.IsZero() || m.last.After(horizon) {
			all = append(all, model.ModelSeen{ID: m.id, Last: m.last})
		}
	}
	if len(all) == 0 {
		return "", nil
	}
	// Claude Code writes assistant entries whose model is the placeholder
	// "<synthetic>" (an error or an interruption, not an answer by a model):
	// they are listed with the models seen, but are never "the latest model".
	for _, m := range all {
		if !strings.HasPrefix(m.ID, "<") {
			return m.ID, all
		}
	}
	return "", all
}

func (c *Collector) meta(path string, e os.DirEntry, st *model.Stats) (agentMeta, bool) {
	info, err := e.Info()
	if err != nil {
		return agentMeta{}, false
	}
	k := fileKey{info.ModTime(), info.Size()}
	if cm, ok := c.metas[path]; ok && cm.fileKey == k {
		return cm.m, cm.ok
	}
	cm := cachedMeta{fileKey: k}
	if b, err := readSmall(path, 64<<10); err == nil {
		cm.ok = parseMeta(b, &cm.m)
	}
	c.metas[path] = cm
	st.SubAgentsRead++
	return cm.m, cm.ok
}

// parseMeta decodes an agent-<id>.meta.json.
func parseMeta(b []byte, m *agentMeta) bool {
	var v agentMeta
	if err := json.Unmarshal(b, &v); err != nil {
		var te *json.UnmarshalTypeError
		if !errors.As(err, &te) {
			return false
		}
	}
	*m = v
	return true
}
