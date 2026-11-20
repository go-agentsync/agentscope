package usage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-agentsync/agentscope/quota"
)

var (
	now       = time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	weekReset = now.Add(7 * time.Hour).Unix() // Fri 15:00, the weekly reset
)

// entry is one transcript line of an assistant message.
func entry(at time.Time, id, mdl string, out, cr int64) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"id":%q,"model":%q,"content":[{"type":"text","text":"PRIVATE"}],"usage":{"input_tokens":3,"output_tokens":%d,"cache_read_input_tokens":%d}}}`+"\n",
		at.UTC().Format(time.RFC3339Nano), id, mdl, out, cr)
}

func write(t *testing.T, root, name string, lines ...string) string {
	t.Helper()
	p := filepath.Join(root, "projects", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "")), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func total(s *Scanner, fam int) Counts {
	var c Counts
	for _, v := range s.sums {
		c.Out += v[fam].Out
		c.CacheRead += v[fam].CacheRead
	}
	return c
}

func TestScannerCountsOnceAMessageSplitOverSeveralEntries(t *testing.T) {
	root := t.TempDir()
	at := now.Add(-time.Hour)
	write(t, root, "p/a.jsonl",
		entry(at, "m1", "claude-opus-5-5", 10, 100), // the first entry of message m1 ...
		entry(at, "m1", "claude-opus-5-5", 25, 100), // ... and its last: 25 output, counted once
		entry(at, "m1", "claude-opus-5-5", 25, 100), // a repeat adds nothing
		entry(at, "m2", "claude-sonnet-4-6", 7, 70),
		entry(at, "", "claude-haiku-4-5", 4, 40), // no id: nothing to dedup on
		entry(at, "", "claude-haiku-4-5", 4, 40),
		entry(at, "m3", "<synthetic>", 9, 9),                             // not a model
		entry(at, "m4", "claude-fable-1", 1, 2),                          // Fable
		entry(at.Add(-40*24*time.Hour), "m5", "claude-opus-5-5", 99, 99), // before the window
		`{"type":"user","message":{"content":"PRIVATE usage"}}`+"\n",
		"not json at all with \"type\":\"assistant\" \"usage\"\n",
	)
	// The same message id in ANOTHER file is another message: both are counted.
	write(t, root, "p/b.jsonl", entry(at, "m1", "claude-opus-5-5", 5, 50))
	s := NewScanner(filepath.Join(root, "projects"), "")
	st := s.Refresh(now)
	if got := total(s, 0); got.Out != 30 || got.CacheRead != 150 {
		t.Fatalf("Opus %+v, want 25+5 output and 100+50 cache", got)
	}
	if got := total(s, 1); got.Out != 7 || got.CacheRead != 70 {
		t.Fatalf("Sonnet %+v", got)
	}
	if got := total(s, 2); got.Out != 8 || got.CacheRead != 80 {
		t.Fatalf("Haiku %+v: entries without an id are each counted", got)
	}
	if got := total(s, 3); got.Out != 1 || got.CacheRead != 2 {
		t.Fatalf("Fable %+v", got)
	}
	if st.Files != 2 || st.Scanned != 2 || st.Bad != 1 || st.Lines == 0 {
		t.Fatalf("%+v", st)
	}
}

func TestScannerIsIncrementalAndSurvivesARestart(t *testing.T) {
	root, cache := t.TempDir(), filepath.Join(t.TempDir(), "c", "usage.json")
	at := now.Add(-time.Hour)
	p := write(t, root, "p/a.jsonl", entry(at, "m1", "claude-opus-5-5", 10, 0))
	s := NewScanner(filepath.Join(root, "projects"), cache)
	s.Refresh(now)
	if st := s.Refresh(now); st.Scanned != 0 {
		t.Fatalf("an unchanged file was read again: %+v", st)
	}

	// A line still being written (no newline) is left for the next pass.
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	line := entry(at, "m2", "claude-opus-5-5", 5, 0)
	f.WriteString(line[:len(line)/2])
	f.Close()
	s.Refresh(now)
	if got := total(s, 0).Out; got != 10 {
		t.Fatalf("a torn line was counted: %d", got)
	}
	f, _ = os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(line[len(line)/2:])
	f.Close()
	st := s.Refresh(now)
	if got := total(s, 0).Out; got != 15 || st.Bytes > int64(len(line))+8 {
		t.Fatalf("%d, read %d bytes", got, st.Bytes)
	}

	// A new process reads the cache and only what was appended since.
	s2 := NewScanner(filepath.Join(root, "projects"), cache)
	if got := total(s2, 0).Out; got != 15 {
		t.Fatalf("cache not loaded: %d", got)
	}
	if st := s2.Refresh(now); st.Scanned != 0 {
		t.Fatalf("a restart re-read the transcripts: %+v", st)
	}

	// A corrupt or foreign cache is ignored, not trusted.
	os.WriteFile(cache, []byte(`{"v":99}`), 0o600)
	if s3 := NewScanner(filepath.Join(root, "projects"), cache); len(s3.sums) != 0 {
		t.Fatal("a cache of another version was used")
	}
	os.WriteFile(cache, []byte(`garbage`), 0o600)
	NewScanner(filepath.Join(root, "projects"), cache).Refresh(now)
}

func TestScannerRestartsAShrunkFileDropsGoneFilesAndOldSums(t *testing.T) {
	root := t.TempDir()
	at := now.Add(-time.Hour)
	p := write(t, root, "p/a.jsonl", entry(at, "m1", "claude-opus-5-5", 10, 0), entry(at, "m2", "claude-opus-5-5", 10, 0))
	old := write(t, root, "p/old.jsonl", entry(now.Add(-60*24*time.Hour), "m1", "claude-opus-5-5", 10, 0))
	os.Chtimes(old, now.Add(-60*24*time.Hour), now.Add(-60*24*time.Hour))
	gone := write(t, root, "p/gone.jsonl", entry(at, "m9", "claude-opus-5-5", 1, 0))
	s := NewScanner(filepath.Join(root, "projects"), "")
	if st := s.Refresh(now); st.Files != 2 {
		t.Fatalf("a transcript untouched for 60 days was opened: %+v", st)
	}
	os.Remove(gone)
	os.WriteFile(p, []byte(entry(at, "m3", "claude-opus-5-5", 3, 0)), 0o600) // rewritten, shorter
	s.Refresh(now)
	if _, ok := s.files[gone]; ok {
		t.Fatal("a vanished file is still tracked")
	}
	if got := total(s, 0).Out; got != 21+3 && got != 3+21 {
		// the old sums stay (20 + 1 from the vanished file) and the rewrite adds 3
		t.Fatalf("%d", got)
	}
	// Sums older than Keep are dropped.
	s.Refresh(now.Add(Keep + 24*time.Hour))
	if len(s.sums) != 0 {
		t.Fatalf("%d buckets kept past Keep", len(s.sums))
	}
}

func TestScannerSkipsALineOverTheBound(t *testing.T) {
	root := t.TempDir()
	at := now.Add(-time.Hour)
	huge := `{"type":"assistant","usage":"` + strings.Repeat("x", maxLine+10) + "\"}\n"
	write(t, root, "p/a.jsonl", huge, entry(at, "m1", "claude-opus-5-5", 2, 0))
	s := NewScanner(filepath.Join(root, "projects"), "")
	st := s.Refresh(now)
	if st.Skipped != 1 || total(s, 0).Out != 2 {
		t.Fatalf("%+v out=%d", st, total(s, 0).Out)
	}
}

func TestReadLineAtTheEndOfInput(t *testing.T) {
	br := bytesReader("a\nb")
	if l, _, ok := readLine(br); !ok || string(l) != "a\n" {
		t.Fatal(string(l), ok)
	}
	if _, _, ok := readLine(br); ok {
		t.Fatal("a line without a newline is complete")
	}
}

// buildAt builds the view at now from the transcripts under root, aiquota having
// reported the weekly reset and a session reset 3 hours away.
func buildAt(t *testing.T, root string, hist []quota.Reading, latest quota.Reading, have bool, qerr string) *model.Usage {
	t.Helper()
	s := NewScanner(filepath.Join(root, "projects"), "")
	s.Refresh(now)
	return Build(now, latest, have, qerr, hist, s)
}

func reading(at int64, weekPct float64) quota.Reading {
	return quota.Reading{At: at, Windows: []quota.Window{
		{Label: quota.LabelSession, Percent: 2, ResetsAt: now.Add(3 * time.Hour).Unix(), Available: true},
		{Label: quota.LabelWeek, Percent: weekPct, ResetsAt: weekReset, Available: true},
		{Label: "Weekly Sonnet"}, // not available
	}}
}

func TestBuildAlignsFourWeeksOnTheResetAndSplitsAtTheBoundary(t *testing.T) {
	root := t.TempDir()
	r := time.Unix(weekReset, 0)
	weekStart := r.Add(-7 * 24 * time.Hour)
	write(t, root, "p/a.jsonl",
		entry(weekStart.Add(-time.Second), "a", "claude-opus-5-5", 100, 0), // last second of the previous week
		entry(weekStart, "b", "claude-opus-5-5", 7, 70),                    // first second of the current one
		entry(weekStart.Add(2*time.Hour), "c", "claude-sonnet-4-6", 3, 30),
		entry(r.Add(-28*24*time.Hour), "d", "claude-haiku-4-5", 5, 50),               // first second of the oldest week
		entry(r.Add(-28*24*time.Hour-time.Second), "e", "claude-haiku-4-5", 1000, 0), // just before all four
	)
	u := buildAt(t, root, []quota.Reading{reading(weekStart.Add(-3*24*time.Hour).Unix(), 41)}, reading(now.Unix(), 54), true, "")
	if u.WeekEnd != weekReset || u.ResetSource != "aiquota" || u.SessionEnd != now.Add(3*time.Hour).Unix() {
		t.Fatalf("%+v", u)
	}
	if len(u.Weeks) != 4 {
		t.Fatal(len(u.Weeks))
	}
	prev, cur, oldest := u.Weeks[2], u.Weeks[3], u.Weeks[0]
	if prev.Out[0] != 100 || cur.Out[0] != 7 || cur.CacheRead[0] != 70 || cur.Out[1] != 3 {
		t.Fatalf("boundary: prev %+v cur %+v", prev, cur)
	}
	if oldest.Out[2] != 5 {
		t.Fatalf("the oldest week starts at the reset 28 days back: %+v", oldest)
	}
	if !cur.InProgress || prev.InProgress {
		t.Fatal("only the current week is in progress")
	}
	if n := len(cur.CumOut); n == 0 || cur.CumOut[n-1] != 10 || n >= week/cumStep {
		t.Fatalf("an in-progress week's line stops at the present: %v", cur.CumOut)
	}
	if n := len(prev.CumOut); n != week/cumStep || prev.CumOut[n-1] != 100 {
		t.Fatalf("a finished week has all its points: %v", prev.CumOut)
	}
	if !cur.Recorded || cur.Percent != 54 || prev.Recorded != true || prev.Percent != 41 || oldest.Recorded {
		t.Fatalf("official percentages: %+v %+v %+v", cur, prev, oldest)
	}
	if u.Short.Start != u.SessionEnd-5*3600 || len(u.Short.Out[0]) != 30 || len(u.Week.Out[0]) != 168 {
		t.Fatalf("spans %+v", u.Short)
	}
	var sum float64
	for _, v := range u.Week.Out[0] {
		sum += v
	}
	if sum != 7 {
		t.Fatalf("the 7 day window holds %v Opus output, want the current week's 7", sum)
	}
}

func TestBuildWindowsHistoryAndNotAvailable(t *testing.T) {
	root := t.TempDir()
	h := []quota.Reading{reading(now.Add(-26*time.Hour).Unix(), 10), reading(now.Add(-25*time.Hour-time.Minute).Unix(), 11), reading(now.Add(-25*time.Hour).Unix(), 12), reading(now.Add(-time.Hour).Unix(), 50), reading(now.Add(-5*time.Minute).Unix(), 53)}
	u := buildAt(t, root, h, reading(now.Unix(), 54), true, "")
	if len(u.Windows) != 3 || u.RecordingSince != h[0].At {
		t.Fatalf("%+v", u)
	}
	var week, sonnet model.QuotaWindow
	for _, w := range u.Windows {
		switch w.Label {
		case quota.LabelWeek:
			week = w
		case "Weekly Sonnet":
			sonnet = w
		}
	}
	if sonnet.Available || len(sonnet.History) != 0 {
		t.Fatalf("an N/A window drew history: %+v", sonnet)
	}
	// One point per hour (the later of two in the same hour), the gap left empty.
	if len(week.History) != 4 || week.History[0].Percent != 11 || week.History[len(week.History)-1].Percent != 54 {
		t.Fatalf("%+v", week.History)
	}
	for _, w := range u.Windows {
		if w.Label == quota.LabelSession && len(w.History) != 3 {
			t.Fatalf("the session window keeps every reading of the last 6 hours: %+v", w.History)
		}
	}
}

func TestBuildFallsBackWhenAiquotaCannotBeRead(t *testing.T) {
	root := t.TempDir()
	// Recorded only: the reset is the last one recorded, said so.
	last := reading(now.Add(-time.Hour).Unix(), 30)
	u := buildAt(t, root, []quota.Reading{last}, last, true, "aiquota not found")
	if u.ResetSource != "recorded" || u.QuotaErr == "" || u.WeekEnd != weekReset {
		t.Fatalf("%+v", u)
	}
	// Nothing known at all: the weeks end now, and there is no source to name.
	u = buildAt(t, root, nil, quota.Reading{}, false, "aiquota not found")
	if u.ResetSource != "" || u.WeekEnd != now.Unix() || len(u.Windows) != 0 || u.RecordingSince != 0 {
		t.Fatalf("%+v", u)
	}
	// A recorded reset in the past is stepped forward by whole weeks.
	stale := quota.Reading{At: now.Add(-20 * 24 * time.Hour).Unix(), Windows: []quota.Window{{Label: quota.LabelWeek, Available: true, ResetsAt: weekReset - 21*24*3600}}}
	u = buildAt(t, root, []quota.Reading{stale}, quota.Reading{}, false, "x")
	if u.ResetSource != "recorded" || u.WeekEnd != weekReset || u.WeekEnd <= now.Unix() {
		t.Fatalf("%d vs %d", u.WeekEnd, weekReset)
	}
	// The last reading has an expired session reset: the 5 hour span is the last 5 hours.
	exp := quota.Reading{At: now.Unix(), Windows: []quota.Window{{Label: quota.LabelSession, Available: true, ResetsAt: now.Unix() - 60}}}
	if u = buildAt(t, root, nil, exp, true, ""); u.SessionEnd != 0 || u.Short.Start > now.Unix()-4*3600 {
		t.Fatalf("%+v", u.Short)
	}
}

type fakeSource struct{ snap model.Snapshot }

func (f fakeSource) Snapshot() model.Snapshot { return f.snap }

func TestEngineReadsAiquotaAtMostEveryFiveMinutesAndRecords(t *testing.T) {
	root, cache, cfg := t.TempDir(), t.TempDir(), t.TempDir()
	write(t, root, "p/a.jsonl", entry(now.Add(-time.Hour), "m", "claude-opus-5-5", 4, 0))
	calls := 0
	var fail error
	e := NewEngine(root, cache, cfg, func(context.Context) ([]byte, error) {
		calls++
		if fail != nil {
			return nil, fail
		}
		return []byte(fmt.Sprintf(`{"at":%q,"windows":[{"label":"Weekly (7 day)","used_percent":54,"resets_at_unix":%d,"verdict":"available"}]}`,
			now.Format(time.RFC3339), weekReset)), nil
	})
	if e.View() != nil {
		t.Fatal("a view before the first pass")
	}
	ctx := context.Background()
	e.Step(ctx, now)
	e.Step(ctx, now.Add(time.Minute)) // within 5 minutes: no second reading
	if calls != 1 {
		t.Fatalf("aiquota read %d times in a minute", calls)
	}
	v := e.View()
	if v == nil || v.ResetSource != "aiquota" || v.Weeks[3].Out[0] != 4 || len(v.Windows) != 1 {
		t.Fatalf("%+v", v)
	}
	if rs, _ := quota.NewStore(cfg).Load(); len(rs) != 1 {
		t.Fatalf("%d readings recorded", len(rs))
	}
	if v.Weeks[3].Percent != 54 || len(v.Windows[0].History) != 1 {
		t.Fatal("the reading just taken was drawn twice or not at all")
	}

	// aiquota fails later: the last reading stands, the error is said.
	fail = errors.New("boom")
	e.Step(ctx, now.Add(6*time.Minute))
	if v = e.View(); v.QuotaErr != "boom" || len(v.Windows) != 1 {
		t.Fatalf("%+v", v)
	}
	// A restart with aiquota down: the history alone gives the reading.
	e2 := NewEngine(root, cache, cfg, func(context.Context) ([]byte, error) { return nil, fail })
	e2.Step(ctx, now.Add(7*time.Minute))
	if v = e2.View(); v.ResetSource != "recorded" || len(v.Windows) != 1 {
		t.Fatalf("%+v", v)
	}
	// Output that is not a reading is an error too.
	e3 := NewEngine(root, cache, t.TempDir(), func(context.Context) ([]byte, error) { return []byte("{"), nil })
	e3.Step(ctx, now)
	if e3.View().QuotaErr == "" {
		t.Fatal("garbage accepted")
	}
	// A history that cannot be written is said, not hidden.
	file := filepath.Join(t.TempDir(), "f")
	os.WriteFile(file, nil, 0o600)
	e4 := NewEngine(root, cache, filepath.Join(file, "x"), func(context.Context) ([]byte, error) {
		return []byte(fmt.Sprintf(`{"at":%q,"windows":[{"label":"x","verdict":"available"}]}`, now.Format(time.RFC3339))), nil
	})
	e4.Step(ctx, now)
	if !strings.Contains(e4.View().QuotaErr, "not recorded") {
		t.Fatalf("%q", e4.View().QuotaErr)
	}

	// Source attaches the view to the inner snapshot.
	snap := Source{Inner: fakeSource{model.Snapshot{Version: 1}}, Eng: e}.Snapshot()
	if snap.Version != 1 || snap.Usage == nil {
		t.Fatalf("%+v", snap)
	}
}

func TestEngineRunStepsUntilCancelled(t *testing.T) {
	root := t.TempDir()
	e := NewEngine(root, t.TempDir(), t.TempDir(), func(context.Context) ([]byte, error) { return nil, errors.New("no") })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.Run(ctx); close(done) }()
	for e.View() == nil {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
}

func TestFamilyIndexNeverGuesses(t *testing.T) {
	if familyIndex("claude-opus-5-5") != 0 || familyIndex("claude-fable-1") != 3 || familyIndex("gpt-4") != -1 || familyIndex("<synthetic>") != -1 {
		t.Fatal("family index")
	}
}

func FuzzFold(f *testing.F) {
	f.Add([]byte(entry(now, "m", "claude-opus-5-5", 1, 2)))
	f.Add([]byte(`{"type":"assistant","usage":1,"timestamp":5,"message":{"usage":{"output_tokens":"x"}}}`))
	f.Add([]byte(`{"type":"assistant","message":{"id":"m","model":"claude-opus-5-5","usage":{"output_tokens":-9}},"timestamp":"2026-11-20T07:00:00Z"}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		s := &Scanner{sums: map[int64][]Counts{}}
		var st Stats
		fs := &fileState{}
		s.fold(b, fs, now.Add(-Keep), &st)
		s.fold(b, fs, now.Add(-Keep), &st)
		for _, v := range s.sums {
			for _, c := range v {
				if c.Out < 0 || c.CacheRead < 0 {
					t.Fatal("negative count")
				}
			}
		}
	})
}

func bytesReader(s string) *bufioReader { return newReader(bytes.NewReader([]byte(s))) }
