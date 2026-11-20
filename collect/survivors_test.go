package collect

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
)

// Each test below holds a refusal or an ordering that a mutsweep of v0.9.0's
// code found nothing held.

func TestReadSmallReportsAMissingFileAsMissing(t *testing.T) {
	_, err := readSmall(filepath.Join(t.TempDir(), "absent"), 10)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want the open's own error", err)
	}
}

func TestSnapshotNamesAnUnreadableTranscriptByItsShortID(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads everything")
	}
	f := newFixture(t)
	p := filepath.Join(f.claude, "projects", "-proj", sidA+".jsonl")
	os.Chmod(p, 0)
	t.Cleanup(func() { os.Chmod(p, 0o644) })
	snap := New(f.options()).Snapshot()
	found := false
	for _, an := range snap.Anomalies {
		if an.Kind == model.AnomalyUnreadable && an.Subject == model.ShortID(sidA) {
			found = true
		}
		if an.Kind == model.AnomalyUnreadable && an.Subject == sidA+".jsonl" {
			t.Errorf("the read of an unopenable file got as far as a scan: %+v", an)
		}
	}
	if !found {
		t.Errorf("no unreadable anomaly for %s: %+v", model.ShortID(sidA), snap.Anomalies)
	}
}

func sessionOf(t *testing.T, snap model.Snapshot, id string) model.Session {
	t.Helper()
	for _, s := range snap.Sessions {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no session %s", id)
	return model.Session{}
}

func TestSessionsAreNewestFirstWhateverTheirIDs(t *testing.T) {
	f := newFixture(t)
	proj := filepath.Join(f.claude, "projects", "-proj")
	const older, newer = "00000000-5555-4555-8555-000000000001", "00000000-5555-4555-8555-000000000002"
	f.transcript(proj, older, f.now.Add(-10*time.Minute), `{"type":"user"}`)
	f.transcript(proj, newer, f.now.Add(-5*time.Minute), `{"type":"user"}`)
	// Same moment: the smaller id first.
	const tieA, tieB = "00000000-5555-4555-8555-000000000003", "00000000-5555-4555-8555-000000000004"
	f.transcript(proj, tieB, f.now.Add(-7*time.Minute), `{"type":"user"}`)
	f.transcript(proj, tieA, f.now.Add(-7*time.Minute), `{"type":"user"}`)
	snap := New(f.options()).Snapshot()
	at := map[string]int{}
	for i, s := range snap.Sessions {
		at[s.ID] = i
	}
	if at[newer] > at[older] {
		t.Errorf("newer session at %d, older at %d: ordered by id, not by activity", at[newer], at[older])
	}
	if at[newer] > at[tieA] || at[tieA] > at[older] {
		t.Errorf("positions: newer %d, tie %d, older %d", at[newer], at[tieA], at[older])
	}
	if at[tieA] > at[tieB] {
		t.Errorf("sessions of one moment are not in id order: %d, %d", at[tieA], at[tieB])
	}
}

func writeLease(t *testing.T, dir, name, resource, owner string, acquired, expires time.Time) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"resource": resource, "owner": owner, "pid": 42, "note": name,
		"acquired": acquired, "expires": expires})
	writeFile(t, filepath.Join(dir, name+".lease"), string(b))
}

func TestLeasesAreLiveFirstThenNewestAcquisitionFirst(t *testing.T) {
	f := newFixture(t)
	locks := filepath.Join(f.claude, "agentsync", "locks")
	// A live lease acquired LONG ago, an expired one acquired recently: the live
	// one is listed first anyway.
	writeLease(t, locks, "zlive-old", "go-tex/zlive-old", sidA, f.now.Add(-3*time.Hour), f.now.Add(time.Hour))
	writeLease(t, locks, "aexpired-new", "go-tex/aexpired-new", sidA, f.now.Add(-time.Hour), f.now.Add(-10*time.Minute))
	// Two live ones: the more recent acquisition first, though its resource
	// sorts last.
	writeLease(t, locks, "znew", "go-tex/znew", sidA, f.now.Add(-30*time.Minute), f.now.Add(time.Hour))
	writeLease(t, locks, "aold", "go-tex/aold", sidA, f.now.Add(-2*time.Hour), f.now.Add(time.Hour))
	snap := New(f.options()).Snapshot()
	at := map[string]int{}
	for i, l := range snap.Leases {
		at[l.Resource] = i
	}
	if at["go-tex/zlive-old"] > at["go-tex/aexpired-new"] {
		t.Errorf("an expired lease is listed before a live one: %v", at)
	}
	if at["go-tex/znew"] > at["go-tex/aold"] {
		t.Errorf("acquisition order ignored: %v", at)
	}
	if at["go-tex/znew"] > at["go-tex/zlive-old"] {
		t.Errorf("a recent live lease is listed after an older live one: %v", at)
	}
}

func TestSubAgentsAreNewestFirstWhateverTheirIDs(t *testing.T) {
	f := newFixture(t)
	sub := filepath.Join(f.claude, "projects", "-proj", sidA, "subagents")
	writeFile(t, filepath.Join(sub, "agent-zx.meta.json"), `{"agentType":"t"}`)
	f.chtimes(filepath.Join(sub, "agent-zx.jsonl"), f.now.Add(-10*time.Second), "{}\n")
	writeFile(t, filepath.Join(sub, "agent-ax.meta.json"), `{"agentType":"t"}`)
	f.chtimes(filepath.Join(sub, "agent-ax.jsonl"), f.now.Add(-20*time.Minute), "{}\n")
	// Two at one moment: the smaller id first.
	writeFile(t, filepath.Join(sub, "agent-m2.meta.json"), `{"agentType":"t"}`)
	f.chtimes(filepath.Join(sub, "agent-m2.jsonl"), f.now.Add(-9*time.Minute), "{}\n")
	writeFile(t, filepath.Join(sub, "agent-m1.meta.json"), `{"agentType":"t"}`)
	f.chtimes(filepath.Join(sub, "agent-m1.jsonl"), f.now.Add(-9*time.Minute), "{}\n")
	se := sessionOf(t, New(f.options()).Snapshot(), sidA)
	at := map[string]int{}
	for i, a := range se.Agents {
		at[a.ID] = i
	}
	if at["zx"] > at["ax"] {
		t.Errorf("an older sub-agent is listed before a newer one: %v", at)
	}
	if at["m1"] > at["m2"] {
		t.Errorf("sub-agents of one moment are not in id order: %v", at)
	}
	if !(at["zx"] < at["m1"] && at["m1"] < at["ax"]) {
		t.Errorf("not newest first: %v", at)
	}
}

func TestAnomaliesAreSortedByKindThenSubject(t *testing.T) {
	f := newFixture(t)
	snap := New(f.options()).Snapshot()
	kinds := map[string]bool{}
	for _, a := range snap.Anomalies {
		kinds[a.Kind] = true
	}
	if len(kinds) < 3 {
		t.Fatalf("the fixture raises %d kinds of anomaly, too few to hold an ordering: %+v", len(kinds), snap.Anomalies)
	}
	if !sort.SliceIsSorted(snap.Anomalies, func(i, j int) bool {
		a, b := snap.Anomalies[i], snap.Anomalies[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Subject < b.Subject
	}) {
		t.Errorf("anomalies not sorted by kind then subject: %+v", snap.Anomalies)
	}
}

func TestModelsOfAnEmptyListIsNothing(t *testing.T) {
	m, all := modelsOf(&tstate{}, time.Now())
	if m != "" || all != nil {
		t.Fatalf("%q %v", m, all)
	}
	old := &tstate{models: []modelSeen{{id: "claude-x", last: time.Now().Add(-time.Hour)}}}
	if m, all := modelsOf(old, time.Now()); m != "" || all != nil {
		t.Fatalf("a model seen before the horizon: %q %v", m, all)
	}
}

func TestDiscardLineStopsAtTheNewlineAndAtTheEnd(t *testing.T) {
	long := strings.Repeat("x", 200_000) // past the 4096-byte reader buffer
	for _, c := range []struct {
		name, in string
		n        int64
		err      error
	}{
		{"a short line", "ab\ncd", 3, nil},
		{"a line past the buffer", long + "\nrest", int64(len(long)) + 1, nil},
		{"no newline at all", long, int64(len(long)), errors.New("EOF")},
	} {
		br := bufio.NewReaderSize(strings.NewReader(c.in), 4096)
		n, err := discardLine(br)
		if n != c.n || (err == nil) != (c.err == nil) {
			t.Errorf("%s: n = %d, err = %v; want %d, %v", c.name, n, err, c.n, c.err)
		}
	}
}

func TestReadLineKeepsTheShortOnesAndConsumesTheOverlongAsNil(t *testing.T) {
	br := bufio.NewReaderSize(strings.NewReader("ab\r\n"+strings.Repeat("y", 50)+"\nlast\n"), 4096)
	line, n, ok, err := readLine(br, 10)
	if string(line) != "ab" || n != 4 || !ok || err != nil {
		t.Fatalf("%q %d %v %v", line, n, ok, err)
	}
	line, n, ok, err = readLine(br, 10)
	if line != nil || n != 51 || !ok || err != nil {
		t.Fatalf("an overlong line: %q (nil %v) %d %v %v", line, line == nil, n, ok, err)
	}
	line, _, ok, _ = readLine(br, 10)
	if string(line) != "last" || !ok {
		t.Fatalf("the line after an overlong one: %q %v", line, ok)
	}
}
