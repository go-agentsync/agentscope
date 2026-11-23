package collect

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
)

func hasAnomaly(s model.Snapshot, kind, subject string) bool {
	for _, a := range s.Anomalies {
		if a.Kind == kind && (subject == "" || a.Subject == subject) {
			return true
		}
	}
	return false
}

func skipWithoutSymlinks(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege on Windows")
	}
}

func TestDefaultOptionsReportAHomeThatCannotBeFound(t *testing.T) {
	for _, k := range []string{"HOME", "USERPROFILE"} {
		t.Setenv(k, "")
	}
	if _, err := DefaultOptions(); err == nil {
		t.Fatal("no home directory, and no error")
	}
}

// A projects or locks directory that cannot be listed for another reason than
// being absent is an anomaly, not an empty tree; a stray file among the
// projects is not a project.
func TestADirectoryThatIsAFileIsReportedAndAStrayFileIgnored(t *testing.T) {
	f := newFixture(t)
	proj := filepath.Join(f.claude, "projects")
	writeFile(t, filepath.Join(proj, "stray.txt"), "x")
	if snap := New(f.options()).Snapshot(); hasAnomaly(snap, model.AnomalyUnreadable, "") {
		t.Fatalf("a stray file is an anomaly: %+v", snap.Anomalies)
	}
	if err := os.RemoveAll(proj); err != nil {
		t.Fatal(err)
	}
	writeFile(t, proj, "not a directory")
	locks := filepath.Join(f.claude, "agentsync", "locks")
	if err := os.RemoveAll(locks); err != nil {
		t.Fatal(err)
	}
	writeFile(t, locks, "not a directory")
	snap := New(f.options()).Snapshot()
	if !hasAnomaly(snap, model.AnomalyUnreadable, proj) || !hasAnomaly(snap, model.AnomalyUnreadable, locks) {
		t.Fatalf("%+v", snap.Anomalies)
	}
	if snap.Stats.ReadErrors < 2 {
		t.Fatalf("ReadErrors = %d", snap.Stats.ReadErrors)
	}
}

// Whatever vanishes between the directory read and the stat is skipped.
func TestEntriesThatCannotBeStatedAreSkipped(t *testing.T) {
	f := newFixture(t)
	old := entryInfo
	defer func() { entryInfo = old }()
	entryInfo = func(fs.DirEntry) (fs.FileInfo, error) { return nil, errors.New("vanished") }
	snap := New(f.options()).Snapshot()
	if snap.Stats.TranscriptsSeen != 0 || snap.Stats.LeasesRead != 0 || snap.Stats.TellsRead != 0 || snap.Stats.SessionRecords != 0 {
		t.Fatalf("%+v", snap.Stats)
	}
}

// Of one session found in two projects, the newest transcript is the one read.
func TestTheOlderCopyOfATranscriptIsIgnored(t *testing.T) {
	f := newFixture(t)
	f.transcript(filepath.Join(f.claude, "projects", "-proj2"), sidA, f.now.Add(-5*time.Hour), `{"type":"ai-title","aiTitle":"Older copy"}`)
	snap := New(f.options()).Snapshot()
	for _, s := range snap.Sessions {
		if s.ID == sidA && s.Title != "Title A" {
			t.Fatalf("title %q", s.Title)
		}
	}
}

// A transcript that cannot be read to its end is an anomaly of that file.
func TestATranscriptThatCannotBeReadIsReported(t *testing.T) {
	f := newFixture(t)
	const sidE = "eeeeeeee-5555-4555-8555-555555555555"
	if err := os.MkdirAll(filepath.Join(f.claude, "projects", "-proj", sidE+".jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	snap := New(f.options()).Snapshot()
	if !hasAnomaly(snap, model.AnomalyUnreadable, sidE+".jsonl") {
		t.Fatalf("%+v", snap.Anomalies)
	}
}

func TestOnlyMaxAgentsSubAgentsAreListed(t *testing.T) {
	f := newFixture(t)
	sub := filepath.Join(f.claude, "projects", "-proj", sidA, "subagents")
	writeFile(t, filepath.Join(sub, "agent-a4.meta.json"), `{"agentType":"Explore"}`)
	f.chtimes(filepath.Join(sub, "agent-a4.jsonl"), f.now.Add(-2*time.Minute), "{}\n")
	o := f.options()
	o.MaxAgents = 1
	for _, s := range New(o).Snapshot().Sessions {
		if s.ID == sidA && len(s.Agents) != 1 {
			t.Fatalf("%d sub-agents listed", len(s.Agents))
		}
	}
}

// A tell file or a session record that is listed but cannot be opened counts as
// a read error, not as an empty file.
func TestAFileThatCannotBeOpenedCountsAsAReadError(t *testing.T) {
	skipWithoutSymlinks(t)
	f := newFixture(t)
	locks := filepath.Join(f.claude, "agentsync", "locks")
	sessions := filepath.Join(f.claude, "agentsync", "sessions")
	const sidE = "eeeeeeee-5555-4555-8555-555555555555"
	if err := os.Symlink(filepath.Join(locks, "absent"), filepath.Join(locks, "dangling.messages")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(sessions, "absent"), filepath.Join(sessions, sidE+".json")); err != nil {
		t.Fatal(err)
	}
	base := New(newFixture(t).options()).Snapshot().Stats.ReadErrors
	if got := New(f.options()).Snapshot().Stats.ReadErrors; got != base+2 {
		t.Fatalf("ReadErrors = %d, want %d", got, base+2)
	}
}

func TestTheResolverIgnoresFilesAmongTheClones(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(f.github, "stray.txt"), "x")
	writeFile(t, filepath.Join(f.github, "go-tex", "stray.txt"), "x")
	writeFile(t, filepath.Join(f.local, "stray.txt"), "x")
	r := &Resolver{GithubRoot: f.github, LocalhostRoot: f.local}
	if _, _, path := r.Resolve("go-tex/engine"); path == "" {
		t.Fatal("a clone was lost among stray files")
	}
}

// A file that is shorter than where the last read stopped, while the state
// still holds a bigger offset, is read from its end, not from beyond it.
func TestScanClampsAnOffsetBeyondTheFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, "")
	fh, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	st := tstate{offset: 100}
	if n, err := scan(fh, 0, &st, 0, 1<<20); err != nil || n != 0 || st.offset != 0 {
		t.Fatalf("n=%d err=%v offset=%d", n, err, st.offset)
	}
}

func TestAnAgentDefinitionThatCannotBeReadIsLeftOut(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "agents", "x.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := AgentWindows(dir); len(got) != 0 {
		t.Fatalf("%v", got)
	}
}

func TestAMetaThatCannotBeStatedIsLeftOut(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "agent-a1.meta.json"), `{"agentType":"x"}`)
	ents, err := os.ReadDir(dir)
	if err != nil || len(ents) != 1 {
		t.Fatal(err, ents)
	}
	old := entryInfo
	defer func() { entryInfo = old }()
	entryInfo = func(fs.DirEntry) (fs.FileInfo, error) { return nil, errors.New("vanished") }
	if _, ok := New(Options{}).meta(filepath.Join(dir, ents[0].Name()), ents[0], &model.Stats{}); ok {
		t.Fatal("a meta that cannot be stated was listed")
	}
}
