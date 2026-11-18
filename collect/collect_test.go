package collect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
)

const (
	sidA = "aaaaaaaa-1111-4111-8111-111111111111" // active, holds go-tex/engine
	sidB = "bbbbbbbb-2222-4222-8222-222222222222" // idle, cwd in a clone
	sidC = "cccccccc-3333-4333-8333-333333333333" // owns a lease, no transcript
	sidD = "dddddddd-4444-4444-8444-444444444444" // old: not listed
)

type fixture struct {
	t                           testing.TB
	home, claude, local, github string
	now                         time.Time
}

func newFixture(t testing.TB) *fixture {
	t.Helper()
	home := t.TempDir()
	f := &fixture{t: t, home: home, claude: filepath.Join(home, ".claude"),
		local: filepath.Join(home, "localhost"), github: filepath.Join(home, "github.com"),
		now: time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)}
	for _, d := range []string{"go-tex-engine-diag", "go-widgets-toolkit", "scratch"} {
		os.MkdirAll(filepath.Join(f.local, d), 0o755)
	}
	os.MkdirAll(filepath.Join(f.github, "go-agentsync", "agentsync"), 0o755)
	os.MkdirAll(filepath.Join(f.github, "go-tex", "engine"), 0o755)
	os.MkdirAll(filepath.Join(f.github, "go-tex", ".github"), 0o755)

	proj := filepath.Join(f.claude, "projects", "-proj")
	f.transcript(proj, sidA, f.now.Add(-1*time.Minute),
		`{"type":"user","timestamp":"2026-11-18T11:58:00Z","cwd":"`+filepath.Join(f.local, "go-tex-engine-diag")+`","gitBranch":"fix","slug":"calm-otter","message":{"content":"PRIVATE PROMPT"}}`,
		`{"type":"ai-title","aiTitle":"Title A"}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"SendMessage","input":{"to":"agentx"}}]}}`)
	f.transcript(proj, sidB, f.now.Add(-2*time.Hour),
		`{"type":"user","cwd":"`+filepath.Join(f.github, "go-agentsync", "agentsync", "lease")+`"}`)
	f.transcript(proj, sidD, f.now.Add(-48*time.Hour), `{"cwd":"/x"}`)
	// Sub-agents of A: one recent, one older than the horizon, one broken meta.
	sub := filepath.Join(proj, sidA, "subagents")
	writeFile(f.t, filepath.Join(sub, "agent-a1.meta.json"), `{"agentType":"general-purpose","description":"fix the build","toolUseId":"toolu_1","spawnDepth":1}`)
	writeFile(f.t, filepath.Join(sub, "agent-a1.jsonl"), "{}\n")
	writeFile(f.t, filepath.Join(sub, "agent-a2.meta.json"), `{"agentType":"Explore"}`)
	f.chtimes(filepath.Join(sub, "agent-a2.jsonl"), f.now.Add(-30*time.Hour), "{}\n")
	writeFile(f.t, filepath.Join(sub, "agent-a3.meta.json"), `{broken`)
	writeFile(f.t, filepath.Join(sub, "agent-a3.jsonl"), "{}\n")
	writeFile(f.t, filepath.Join(sub, "agent-..bad.meta.json"), `{"agentType":"x"}`)
	writeFile(f.t, filepath.Join(sub, "notes.txt"), "x")
	os.Chtimes(filepath.Join(sub, "agent-a1.jsonl"), f.now.Add(-time.Minute), f.now.Add(-time.Minute))
	os.Chtimes(filepath.Join(sub, "agent-a3.jsonl"), f.now.Add(-time.Minute), f.now.Add(-time.Minute))
	// Leases.
	locks := filepath.Join(f.claude, "agentsync", "locks")
	f.lease(locks, "go-tex_engine", "go-tex/engine", sidA, "engine work", 30*time.Minute)
	f.lease(locks, "gh", "github.com/go-tex/.github", sidA, "expired recently", -time.Hour)
	f.lease(locks, "old", "go-tex/old", sidA, "expired long ago", -40*time.Hour)
	f.lease(locks, "note", "adding_real_HTTP_2_support", sidC, "polluted", 30*time.Minute)
	f.lease(locks, "oldnote", "some old note", sidD, "ancient", -40*time.Hour)
	f.lease(locks, "topic", "go-tex/engine#m2", sidC, "topic form", 30*time.Minute)
	f.lease(locks, "deep", "go-tex/engine/wave1", sidC, "path form", 30*time.Minute)
	f.lease(locks, "org", "go-tex", sidC, "an org", 30*time.Minute)
	f.lease(locks, "flag", "--note", sidC, "flag", 30*time.Minute)
	writeFile(f.t, filepath.Join(locks, "torn.lease"), `{"resource":"x"`)
	writeFile(f.t, filepath.Join(locks, "incomplete.lease"), `{"resource":"x"}`)
	// Tells: to a session prefix, to a resource, from an unknown session, old.
	msg := func(res, from, text string, at time.Time) string {
		b, _ := json.Marshal(map[string]any{"resource": res, "from": from, "text": text, "at": at})
		return string(b) + "\n"
	}
	writeFile(f.t, filepath.Join(locks, "aaaaaaaa.messages"),
		msg("aaaaaaaa", sidB, "hello\nworld "+strings.Repeat("z", 500), f.now.Add(-time.Hour))+"not json\n"+msg("aaaaaaaa", sidB, "too old", f.now.Add(-72*time.Hour)))
	writeFile(f.t, filepath.Join(locks, "go-tex_engine.messages"), msg("go-tex/engine", sidA, "to a repo", f.now.Add(-time.Minute))+
		msg("99999999", "eeeeeeee-5555-4555-8555-555555555555", "ghost", f.now.Add(-time.Minute)))
	// Session records (content is a memory line and must not be read).
	writeFile(f.t, filepath.Join(f.claude, "agentsync", "sessions", sidA+".json"),
		`[{"name":"n","line":"PRIVATE MEMORY LINE","added":"2026-11-18T09:00:00Z"},{"added":"2026-11-18T10:00:00Z"}]`)
	writeFile(f.t, filepath.Join(f.claude, "agentsync", "sessions", sidC+".json"), `[]`)
	writeFile(f.t, filepath.Join(f.claude, "agentsync", "sessions", "not-a-uuid.json"), `[]`)
	os.Chtimes(filepath.Join(f.claude, "agentsync", "sessions", sidC+".json"), f.now.Add(-time.Hour), f.now.Add(-time.Hour))
	return f
}

func (f *fixture) chtimes(p string, at time.Time, content string) {
	writeFile(f.t, p, content)
	os.Chtimes(p, at, at)
}

func (f *fixture) transcript(dir, id string, mtime time.Time, lines ...string) {
	f.chtimes(filepath.Join(dir, id+".jsonl"), mtime, strings.Join(lines, "\n")+"\n")
}

func (f *fixture) lease(dir, name, resource, owner, note string, ttl time.Duration) {
	b, _ := json.Marshal(map[string]any{"resource": resource, "owner": owner, "pid": 42, "note": note,
		"acquired": f.now.Add(ttl - 45*time.Minute), "expires": f.now.Add(ttl)})
	writeFile(f.t, filepath.Join(dir, name+".lease"), string(b))
}

func (f *fixture) options() Options {
	o, _ := DefaultOptions()
	o.ClaudeDir, o.LocalhostRoot, o.GithubRoot = f.claude, f.local, f.github
	o.Now = func() time.Time { return f.now }
	o.MaxTellText = 60
	return o
}

func TestSnapshotOfAFixtureTree(t *testing.T) {
	f := newFixture(t)
	snap := New(f.options()).Snapshot()

	byID := map[string]model.Session{}
	for _, s := range snap.Sessions {
		byID[s.ID] = s
	}
	if _, ok := byID[sidD]; ok {
		t.Error("a session older than the horizon with no lease must not be listed")
	}
	a, b, c := byID[sidA], byID[sidB], byID[sidC]
	if !a.Active || !a.HasTranscript || a.Title != "Title A" || a.Slug != "calm-otter" || a.Branch != "fix" || a.CwdRepo != "go-tex/engine" || a.Sent != 1 || a.MemAdds != 2 {
		t.Errorf("session A %+v", a)
	}
	if b.Active || b.CwdRepo != "go-agentsync/agentsync" || b.LastActivity.IsZero() {
		t.Errorf("session B %+v", b)
	}
	if c.HasTranscript || c.Active || c.Short != "cccccccc" {
		t.Errorf("session C %+v", c)
	}
	if len(a.Agents) != 2 || a.Agents[0].Type == "" && a.Agents[1].Type == "" {
		t.Errorf("sub-agents %+v (one older than the horizon and one with a broken meta must go)", a.Agents)
	}
	for _, ag := range a.Agents {
		if ag.ID == "a1" && (ag.Type != "general-purpose" || ag.Description != "fix the build" || ag.Depth != 1 || !ag.Active) {
			t.Errorf("agent %+v", ag)
		}
	}
	if snap.Sessions[0].ID != sidA {
		t.Errorf("sessions must be newest first, got %s first", snap.Sessions[0].Short)
	}

	kinds := map[model.RepoKind]int{}
	var live, expired int
	for _, l := range snap.Leases {
		kinds[l.Kind]++
		if l.Expired {
			expired++
		} else {
			live++
		}
		if l.Resource == "go-tex/engine#m2" && (l.Repo != "go-tex/engine" || l.Kind != model.KindRepo) {
			t.Errorf("topic form: %+v", l)
		}
		if l.Resource == "go-tex/engine/wave1" && l.Repo != "go-tex/engine" {
			t.Errorf("path form: %+v", l)
		}
		if l.Resource == "go-tex/old" {
			t.Error("an expired lease older than the horizon must not be listed")
		}
	}
	if live != 6 || expired != 1 || kinds[model.KindUnresolved] != 2 || kinds[model.KindOrg] != 1 {
		t.Errorf("live %d expired %d kinds %v", live, expired, kinds)
	}
	if snap.Leases[0].Expired {
		t.Error("live leases come first")
	}

	if len(snap.Tells) != 3 { // the 72-hour-old tell is outside the window
		t.Fatalf("tells %+v", snap.Tells)
	}
	for _, tl := range snap.Tells {
		if strings.Contains(tl.Text, "\n") || len([]rune(tl.Text)) > 60 {
			t.Errorf("tell text not bounded: %q", tl.Text)
		}
	}

	want := map[string]bool{
		model.AnomalyUnresolvedKey:   false, // --note and the note-as-key lease
		model.AnomalyOrphanLease:     false, // sidC holds leases and has no transcript
		model.AnomalyUnknownSender:   false, // eeeeeeee
		model.AnomalyUnknownTarget:   false, // zzzzzzzz
		model.AnomalyBadLine:         false, // none in transcripts here; set below
		model.AnomalyOversizedLine:   false,
		model.AnomalyTranscriptStall: false,
	}
	for _, an := range snap.Anomalies {
		if _, ok := want[an.Kind]; ok {
			want[an.Kind] = true
		}
	}
	for _, k := range []string{model.AnomalyUnresolvedKey, model.AnomalyOrphanLease, model.AnomalyUnknownSender, model.AnomalyUnknownTarget} {
		if !want[k] {
			t.Errorf("no %q anomaly in %+v", k, snap.Anomalies)
		}
	}
	hist := false
	for _, an := range snap.Anomalies {
		if an.Subject == "(expired before the horizon)" {
			hist = true
		}
	}
	if !hist {
		t.Error("the unresolved keys older than the horizon must be reported in aggregate")
	}
	st := snap.Stats
	if st.LeasesTorn != 2 || st.TranscriptsSeen != 3 || st.ExpiredNotListed != 2 || st.SessionRecords != 2 || st.TellsDropped < 2 {
		t.Errorf("stats %+v", st)
	}

	// Metadata only: no prompt, memory line or reply anywhere in the JSON.
	b2, _ := json.Marshal(snap)
	for _, secret := range []string{"PRIVATE PROMPT", "PRIVATE MEMORY"} {
		if strings.Contains(string(b2), secret) {
			t.Errorf("%q leaked into the snapshot", secret)
		}
	}
}

func TestSnapshotIsIncrementalAndTracksNewTranscriptLines(t *testing.T) {
	f := newFixture(t)
	c := New(f.options())
	first := c.Snapshot()
	p := filepath.Join(f.claude, "projects", "-proj", sidA+".jsonl")
	appendFile(t, p, `{"type":"ai-title","aiTitle":"Retitled"}`+"\n")
	os.Chtimes(p, f.now, f.now)
	second := c.Snapshot()
	var title string
	for _, s := range second.Sessions {
		if s.ID == sidA {
			title = s.Title
		}
	}
	if title != "Retitled" {
		t.Errorf("title %q", title)
	}
	if second.Stats.TranscriptsRead != 1 || second.Stats.BytesRead > 100 {
		t.Errorf("only the appended bytes must be read: %+v (first pass read %d)", second.Stats, first.Stats.BytesRead)
	}
	// A shrunken transcript is reported, and read again.
	writeFile(t, p, `{"type":"ai-title","aiTitle":"T"}`+"\n")
	third := c.Snapshot()
	found := false
	for _, an := range third.Anomalies {
		if an.Kind == model.AnomalyTranscriptStall {
			found = true
		}
	}
	if !found {
		t.Error("a transcript that shrank must be reported")
	}
	// A lease that disappears is forgotten.
	os.Remove(filepath.Join(f.claude, "agentsync", "locks", "go-tex_engine.lease"))
	os.Remove(filepath.Join(f.claude, "agentsync", "locks", "go-tex_engine.messages"))
	os.Remove(filepath.Join(f.claude, "agentsync", "sessions", sidA+".json"))
	fourth := c.Snapshot()
	for _, l := range fourth.Leases {
		if l.Resource == "go-tex/engine" {
			t.Error("the deleted lease is still listed")
		}
	}
}

func TestSnapshotOfAMissingTree(t *testing.T) {
	o, _ := DefaultOptions()
	o.ClaudeDir = filepath.Join(t.TempDir(), "absent")
	o.LocalhostRoot, o.GithubRoot = "", ""
	snap := New(o).Snapshot()
	if len(snap.Sessions) != 0 || len(snap.Anomalies) != 0 || snap.Version != model.Version {
		t.Errorf("%+v", snap)
	}
}

func TestSnapshotReportsUnreadableThings(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads everything")
	}
	f := newFixture(t)
	proj := filepath.Join(f.claude, "projects", "-proj")
	os.Chmod(filepath.Join(proj, sidA+".jsonl"), 0)
	os.Chmod(filepath.Join(f.claude, "projects", "-proj"), 0o755)
	os.MkdirAll(filepath.Join(f.claude, "projects", "-locked"), 0o755)
	os.Chmod(filepath.Join(f.claude, "projects", "-locked"), 0)
	t.Cleanup(func() {
		os.Chmod(filepath.Join(f.claude, "projects", "-locked"), 0o755)
		os.Chmod(filepath.Join(proj, sidA+".jsonl"), 0o644)
	})
	snap := New(f.options()).Snapshot()
	n := 0
	for _, an := range snap.Anomalies {
		if an.Kind == model.AnomalyUnreadable {
			n++
		}
	}
	if n < 2 || snap.Stats.ReadErrors < 2 {
		t.Errorf("unreadable files must be reported, not skipped: %d anomalies, %+v", n, snap.Stats)
	}
}

func TestSnapshotReportsOversizedAndBadTranscriptLines(t *testing.T) {
	f := newFixture(t)
	o := f.options()
	o.MaxLine = 200
	p := filepath.Join(f.claude, "projects", "-proj", sidA+".jsonl")
	appendFile(t, p, strings.Repeat("A", 500)+"\nnot json\n")
	os.Chtimes(p, f.now, f.now)
	snap := New(o).Snapshot()
	got := map[string]bool{}
	for _, an := range snap.Anomalies {
		got[an.Kind] = true
	}
	if !got[model.AnomalyOversizedLine] || !got[model.AnomalyBadLine] {
		t.Errorf("anomalies %+v", snap.Anomalies)
	}
}

func TestDefaultOptions(t *testing.T) {
	o, err := DefaultOptions()
	if err != nil || o.Active != 10*time.Minute || o.ClaudeDir == "" || o.MaxLine <= 0 {
		t.Errorf("%+v %v", o, err)
	}
}

func TestReadSmallBound(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	writeFile(t, p, strings.Repeat("x", 100))
	if _, err := readSmall(p, 99); err == nil {
		t.Error("a file over the bound was read")
	}
	if b, err := readSmall(p, 100); err != nil || len(b) != 100 {
		t.Error(err)
	}
	if _, err := readSmall(p+"-absent", 10); err == nil {
		t.Error("absent file")
	}
	if _, err := readSmall(t.TempDir(), 10); err == nil {
		t.Error("a directory is not a file")
	}
}

func TestIDShapes(t *testing.T) {
	for s, want := range map[string]bool{sidA: true, "AAAAAAAA-1111-4111-8111-111111111111": false, "aaaaaaaa-1111-4111-8111-11111111111": false, "aaaaaaaaX1111-4111-8111-111111111111": false, "aaaaaaaa-1111-4111-8111-11111111111g": false} {
		if isUUID(s) != want {
			t.Errorf("isUUID(%q) != %v", s, want)
		}
	}
	for s, want := range map[string]bool{"aaaaaaaa": true, "aaaaaaa": false, "aaaaaaag": false, "AAAAAAAA": false} {
		if isShort(s) != want {
			t.Errorf("isShort(%q) != %v", s, want)
		}
	}
}

func TestParseHelpers(t *testing.T) {
	if _, ok := parseLease([]byte(`{"resource":"a","owner":"b"}`)); !ok {
		t.Error("lease")
	}
	for _, b := range []string{``, `{`, `{"resource":"a"}`, `{"owner":"a"}`, `[]`} {
		if _, ok := parseLease([]byte(b)); ok {
			t.Errorf("lease accepted %q", b)
		}
	}
	var m agentMeta
	if !parseMeta([]byte(`{"agentType":"t","spawnDepth":"deep"}`), &m) || m.AgentType != "t" {
		t.Error("a type error keeps what decoded")
	}
	if parseMeta([]byte(`{`), &m) {
		t.Error("meta")
	}
	if n, last, ok := parseRecord([]byte(`[{"added":"2026-01-01T00:00:00Z"},{}]`)); !ok || n != 2 || last.IsZero() {
		t.Error("record")
	}
	if _, _, ok := parseRecord([]byte(`{"a":1}`)); ok {
		t.Error("record accepted an object")
	}
	msgs, bad := parseTells(strings.NewReader(`{"resource":"r","from":"f","text":"t","at":"2026-01-01T00:00:00Z"}`+"\n\n  \nnope\n"+strings.Repeat("x", 300)+"\n"+`{"resource":"torn"`), 200)
	if len(msgs) != 1 || bad != 2 {
		t.Errorf("tells %d bad %d", len(msgs), bad)
	}
}
