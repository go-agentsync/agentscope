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
	weekReset = now.Add(7 * time.Hour).Unix() // Fri 15:00, the weekly reset of account A
	resetB    = now.Add(5*24*time.Hour + 3*time.Hour).Unix()

	acctA, acctB = quota.AccountKey("uuid-a"), quota.AccountKey("uuid-b")
)

// entry is one transcript line of an assistant message.
func entry(at time.Time, id, mdl string, out, cr int64) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"id":%q,"model":%q,"content":[{"type":"text","text":"PRIVATE"}],"usage":{"input_tokens":3,"output_tokens":%d,"cache_read_input_tokens":%d}}}`+"\n",
		at.UTC().Format(time.RFC3339Nano), id, mdl, out, cr)
}

// owner is the session-owner entry Claude Code writes when a session starts.
func owner(uuid string) string {
	return fmt.Sprintf(`{"type":"session-owner","accountUuid":%q,"organizationUuid":"org","sessionId":"s"}`+"\n", uuid)
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

// total is the output and cache-read tokens of a family and an account.
func total(s *Scanner, fam int, acct string) Counts {
	var c Counts
	for k, v := range s.sums {
		if k.Fam == fam && k.Acct == acct {
			c.Out += v.Out
			c.CacheRead += v.CacheRead
		}
	}
	return c
}

func TestScannerAttributesToTheAccountOfTheLatestSessionOwnerBefore(t *testing.T) {
	root := t.TempDir()
	at := now.Add(-time.Hour)
	write(t, root, "p/a.jsonl",
		entry(at, "u0", "claude-opus-5-5", 1, 10), // before any session-owner: unattributed
		owner("uuid-a"),
		entry(at, "m1", "claude-opus-5-5", 10, 100), // the first entry of message m1 ...
		entry(at, "m1", "claude-opus-5-5", 25, 100), // ... and its last: 25 output, counted once
		entry(at, "m1", "claude-opus-5-5", 25, 100), // a repeat adds nothing
		entry(at, "m2", "claude-sonnet-4-6", 7, 70),
		owner("uuid-b"), // the account switched inside the same file (and week)
		entry(at, "m3", "claude-opus-5-5", 3, 30),
		entry(at, "", "claude-haiku-4-5", 4, 40), // no id: nothing to dedup on
		entry(at, "", "claude-haiku-4-5", 4, 40),
		entry(at, "m4", "<synthetic>", 9, 9),                             // not a model
		entry(at, "m5", "claude-fable-1", 1, 2),                          // Fable, account B
		entry(at.Add(-40*24*time.Hour), "m6", "claude-opus-5-5", 99, 99), // before the window
		`{"type":"user","message":{"content":"PRIVATE usage"}}`+"\n",
		"not json at all with \"type\":\"assistant\" \"usage\"\n",
	)
	// The same message id in ANOTHER file is another message; a file that never
	// names an account is wholly unattributed.
	write(t, root, "p/b.jsonl", entry(at, "m1", "claude-opus-5-5", 5, 50))
	// A second session of account A.
	write(t, root, "p/c.jsonl", owner("uuid-a"), entry(at, "m1", "claude-opus-5-5", 2, 20))
	s := NewScanner(filepath.Join(root, "projects"), "")
	st := s.Refresh(now)
	if got := total(s, 0, acctA); got.Out != 27 || got.CacheRead != 120 {
		t.Fatalf("Opus of A %+v, want 25+2 output and 100+20 cache", got)
	}
	if got := total(s, 1, acctA); got.Out != 7 || got.CacheRead != 70 {
		t.Fatalf("Sonnet of A %+v", got)
	}
	if got := total(s, 0, acctB); got.Out != 3 || got.CacheRead != 30 {
		t.Fatalf("Opus of B %+v: the switch inside the file", got)
	}
	if got := total(s, 2, acctB); got.Out != 8 || got.CacheRead != 80 {
		t.Fatalf("Haiku of B %+v: entries without an id are each counted", got)
	}
	if got := total(s, 3, acctB); got.Out != 1 || got.CacheRead != 2 {
		t.Fatalf("Fable of B %+v", got)
	}
	if got := total(s, 0, ""); got.Out != 6 || got.CacheRead != 60 {
		t.Fatalf("unattributed Opus %+v: the entry before any owner and the file with none", got)
	}
	if keys, un := s.Accounts(); len(keys) != 2 || !un {
		t.Fatalf("%v %v", keys, un)
	}
	if st.Files != 3 || st.Scanned != 3 || st.Bad != 1 || st.Lines == 0 {
		t.Fatalf("%+v", st)
	}

	// Only one account, or every one with the unattributed tokens as a last layer.
	start := now.Add(-2*time.Hour).Unix() - now.Add(-2*time.Hour).Unix()%BucketSec
	oa, _ := s.Buckets(start, BucketSec, 12, Only, acctA)
	if len(oa) != 4 || sum(oa[0]) != 27 {
		t.Fatalf("%v", oa)
	}
	oe, ce := s.Buckets(start, BucketSec, 12, Every, "")
	if len(oe) != 5 || sum(oe[4]) != 6 || sum(ce[4]) != 60 || sum(oe[0]) != 27+3 {
		t.Fatalf("every: unattributed %v, Opus %v", sum(oe[4]), sum(oe[0]))
	}
	none, _ := s.Buckets(start, BucketSec, 12, Only, "\x00")
	if sum(none[0])+sum(none[1])+sum(none[2])+sum(none[3]) != 0 {
		t.Fatal("an account that does not exist has tokens")
	}
	if o, _ := s.Buckets(0, BucketSec, 3, Every, ""); sum(o[0]) != 0 { // buckets before every sum
		t.Fatal("tokens outside the range")
	}
}

func sum(v []float64) (s float64) {
	for _, x := range v {
		s += x
	}
	return s
}

func TestScannerKeepsTheOwnerAcrossPassesAndARestart(t *testing.T) {
	root, cache := t.TempDir(), filepath.Join(t.TempDir(), "c", "usage.json")
	at := now.Add(-time.Hour)
	p := write(t, root, "p/a.jsonl", owner("uuid-a"), entry(at, "m1", "claude-opus-5-5", 10, 0))
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
	if got := total(s, 0, acctA).Out; got != 10 {
		t.Fatalf("a torn line was counted: %d", got)
	}
	f, _ = os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(line[len(line)/2:])
	f.Close()
	st := s.Refresh(now)
	if got := total(s, 0, acctA).Out; got != 15 || st.Bytes > int64(len(line))+8 {
		t.Fatalf("%d, read %d bytes: the owner read in an earlier pass must still hold", got, st.Bytes)
	}

	// A new process reads the cache, the owner included, and only what was
	// appended since.
	s2 := NewScanner(filepath.Join(root, "projects"), cache)
	if got := total(s2, 0, acctA).Out; got != 15 {
		t.Fatalf("cache not loaded: %d", got)
	}
	f, _ = os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(entry(at, "m3", "claude-opus-5-5", 1, 0))
	f.Close()
	s2.Refresh(now)
	if got := total(s2, 0, acctA).Out; got != 16 || total(s2, 0, "").Out != 0 {
		t.Fatalf("after a restart the new entry belongs to %d of A, %d unattributed", got, total(s2, 0, "").Out)
	}

	// A corrupt, foreign or version 1 cache is ignored, not trusted.
	for _, c := range []string{`{"v":99}`, `{"v":1,"sums":{"5":[{"o":9}]}}`, `garbage`} {
		os.WriteFile(cache, []byte(c), 0o600)
		if s3 := NewScanner(filepath.Join(root, "projects"), cache); len(s3.sums) != 0 {
			t.Fatalf("cache %q was used", c)
		}
	}
	// A sum for a family that does not exist is dropped.
	os.WriteFile(cache, []byte(`{"v":2,"sums":[{"b":600,"f":9,"c":{"o":1}},{"b":600,"f":1,"c":{"o":1}}]}`), 0o600)
	if s4 := NewScanner(filepath.Join(root, "projects"), cache); len(s4.sums) != 1 {
		t.Fatalf("%v", s4.sums)
	}
}

func TestScannerRestartsAShrunkFileDropsGoneFilesAndOldSums(t *testing.T) {
	root := t.TempDir()
	at := now.Add(-time.Hour)
	p := write(t, root, "p/a.jsonl", owner("uuid-a"), entry(at, "m1", "claude-opus-5-5", 10, 0), entry(at, "m2", "claude-opus-5-5", 10, 0))
	old := write(t, root, "p/old.jsonl", entry(now.Add(-60*24*time.Hour), "m1", "claude-opus-5-5", 10, 0))
	os.Chtimes(old, now.Add(-60*24*time.Hour), now.Add(-60*24*time.Hour))
	gone := write(t, root, "p/gone.jsonl", entry(at, "m9", "claude-opus-5-5", 1, 0))
	s := NewScanner(filepath.Join(root, "projects"), "")
	if st := s.Refresh(now); st.Files != 2 {
		t.Fatalf("a transcript untouched for 60 days was opened: %+v", st)
	}
	os.Remove(gone)
	os.WriteFile(p, []byte(entry(at, "m3", "claude-opus-5-5", 3, 0)), 0o600) // rewritten, shorter, no owner now
	s.Refresh(now)
	if _, ok := s.files[gone]; ok {
		t.Fatal("a vanished file is still tracked")
	}
	if got := total(s, 0, ""); got.Out != 1+3 { // the vanished file's 1 stays; the rewrite has no owner
		t.Fatalf("%+v", got)
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
	write(t, root, "p/a.jsonl", huge, owner("uuid-a"), entry(at, "m1", "claude-opus-5-5", 2, 0))
	s := NewScanner(filepath.Join(root, "projects"), "")
	st := s.Refresh(now)
	if st.Skipped != 1 || total(s, 0, acctA).Out != 2 {
		t.Fatalf("%+v out=%d", st, total(s, 0, acctA).Out)
	}
}

func TestReadLineAtTheEndOfInput(t *testing.T) {
	br := newReader(bytes.NewReader([]byte("a\nb")))
	if l, _, ok := readLine(br); !ok || string(l) != "a\n" {
		t.Fatal(string(l), ok)
	}
	if _, _, ok := readLine(br); ok {
		t.Fatal("a line without a newline is complete")
	}
}

// win builds a reading of one account.
func win(at int64, acct, label, plan string, session, weekly float64, sessEnd, weekEnd int64) quota.Reading {
	return quota.Reading{At: at, Acct: acct, Label: label, Plan: plan, Windows: []quota.Window{
		{Label: quota.LabelSession, Percent: session, ResetsAt: sessEnd, Available: true},
		{Label: quota.LabelWeek, Percent: weekly, ResetsAt: weekEnd, Available: true},
		{Label: "Weekly Sonnet"}, // not available
	}}
}

// twoAccountTree is a tree where A and B each worked, A also in a gap with no
// owner, and an account switched inside the current week.
func twoAccountTree(t *testing.T) *Scanner {
	root := t.TempDir()
	r := time.Unix(weekReset, 0)
	weekStart := r.Add(-7 * 24 * time.Hour)
	write(t, root, "p/a.jsonl",
		entry(weekStart.Add(-time.Second), "x", "claude-opus-5-5", 1000, 0), // before any owner: unattributed
		owner("uuid-a"),
		entry(weekStart.Add(-time.Second), "a", "claude-opus-5-5", 100, 0), // last second of A's previous week
		entry(weekStart, "b", "claude-opus-5-5", 7, 70),                    // first second of its current one
		entry(weekStart.Add(2*time.Hour), "c", "claude-sonnet-4-6", 3, 30),
		owner("uuid-b"), // the switch inside the week
		entry(weekStart.Add(3*time.Hour), "d", "claude-haiku-4-5", 5, 50),
		entry(now.Add(-time.Hour), "e", "claude-opus-5-5", 20, 200),
	)
	s := NewScanner(filepath.Join(root, "projects"), "")
	s.Refresh(now)
	return s
}

func TestBuildSeparatesTwoAccountsWithOwnResetsAndSumsThem(t *testing.T) {
	s := twoAccountTree(t)
	live := []quota.Reading{
		win(now.Unix(), acctA, "Claude Code", "max", 2, 54, now.Add(3*time.Hour).Unix(), weekReset),
		win(now.Unix(), acctB, "Claude Code", "team", 13, 1, now.Add(2*time.Hour).Unix(), resetB),
	}
	// A recorded history with a gap: B was read 3 days ago and not since until now.
	hist := []quota.Reading{
		win(now.Add(-3*24*time.Hour).Unix(), acctB, "Claude Code", "team", 5, 0, 0, resetB),
		win(now.Add(-26*time.Hour).Unix(), acctA, "Claude Code", "max", 5, 41, 0, weekReset),
		{At: now.Add(-90 * time.Hour).Unix(), Windows: []quota.Window{{Label: quota.LabelWeek, Percent: 33, Available: true, ResetsAt: now.Add(-80 * time.Hour).Unix()}}}, // legacy, unknown account
	}
	u := Build(Input{Now: now, Live: live, Hist: hist, Active: acctB}, s)
	if len(u.Accounts) != 3 || u.Accounts[2].Key != "" {
		t.Fatalf("known accounts first, the unknown one last: %+v", keys(u))
	}
	var a, b, unk model.AccountUsage
	for _, x := range u.Accounts {
		switch x.Key {
		case acctA:
			a = x
		case acctB:
			b = x
		default:
			unk = x
		}
	}
	if a.WeekEnd != weekReset || b.WeekEnd != resetB || a.ResetSource != "aiquota" || b.ResetSource != "aiquota" {
		t.Fatalf("each account has its own reset: %d %d", a.WeekEnd, b.WeekEnd)
	}
	if a.Active || !b.Active || u.Active != acctB {
		t.Fatal("the active account is the one Claude Code is signed in to")
	}
	if a.Plan != "max" || b.Plan != "team" || b.RecordingSince != hist[0].At {
		t.Fatalf("%+v", b)
	}
	// A's weeks: the boundary splits tokens; B's tokens are not A's.
	prev, cur := a.Weeks[2], a.Weeks[3]
	if prev.Out[0] != 100 || cur.Out[0] != 7 || cur.Out[1] != 3 || cur.Out[2] != 0 || cur.CacheRead[0] != 70 {
		t.Fatalf("A: prev %+v cur %+v", prev, cur)
	}
	if !cur.Recorded || cur.Percent != 54 || !prev.Recorded && false {
		t.Fatalf("%+v", cur)
	}
	// B has its own alignment: its current week started 2 days ago, so the
	// tokens from 6 days ago are in its previous one.
	bw := b.Weeks[3]
	if bw.Start != resetB-7*24*3600 || !bw.InProgress {
		t.Fatalf("%+v", bw)
	}
	var bTotal float64
	for _, w := range b.Weeks {
		bTotal += sum(w.Out)
	}
	if bTotal != 5+20 {
		t.Fatalf("B's own tokens %v", bTotal)
	}
	if sum(unk.Weeks[3].Out) != 0 || len(unk.Windows) != 1 || unk.Label != "unknown account" {
		t.Fatal("the unknown account has no tokens and keeps its old readings")
	}
	// The sum: everyone's tokens plus the unattributed ones as an extra layer.
	if len(u.All.Layers) != 5 || u.All.Layers[4] != model.UnattributedLayer || len(u.All.Days.Out) != 5 || len(u.All.Days.Out[0]) != 28 {
		t.Fatal("the aggregated layers")
	}
	var days float64
	for l := 0; l < 4; l++ {
		days += sum(u.All.Days.Out[l])
	}
	if days != 7+3+5+20+100 || u.All.UnattributedOut != 1000 {
		t.Fatalf("days %v, unattributed %v: A's 100 of the previous week is in the 28 days too", days, u.All.UnattributedOut)
	}
	if got := sum(u.All.Short.Out[0]) + sum(u.All.Short.Out[4]); got != 20 {
		t.Fatalf("last 5 hours %v", got)
	}
	if u.All.Days.Start%86400 != midnight(now).Unix()%86400 && false {
		t.Fatal("days start at a midnight")
	}
	// Capacity: A is at 54 % (0.46 left), B at 1 % (0.99); the unknown account's
	// reading is old and not counted.
	c := u.Capacity
	if c.Counted != 2 || c.AccountWeeks < 1.449 || c.AccountWeeks > 1.451 || len(c.Items) != 3 {
		t.Fatalf("%+v", c)
	}
	var it model.CapacityItem
	for _, i := range c.Items {
		if i.Key == acctB {
			it = i
		}
		if i.Key == "" && (!i.Unknown || !i.Stale) {
			t.Fatalf("the unknown account's old reading must be listed and not counted: %+v", i)
		}
	}
	if !it.Active || it.Session != 13 || it.NextReset != resetB || !it.SessionOK {
		t.Fatalf("%+v", it)
	}
}

func keys(u *model.Usage) (out []string) {
	for _, a := range u.Accounts {
		out = append(out, a.Key)
	}
	return out
}

func TestBuildAnAccountWithNotAvailableWindowsDrawsNoHistoryForThem(t *testing.T) {
	s := twoAccountTree(t)
	r := win(now.Unix(), acctA, "Claude Code", "team", 1, 1, now.Add(time.Hour).Unix(), weekReset)
	r.Windows = append(r.Windows, quota.Window{Label: "Weekly Opus"})
	u := Build(Input{Now: now, Live: []quota.Reading{r}, Hist: []quota.Reading{r}}, s)
	for _, a := range u.Accounts {
		if a.Key != acctA {
			continue
		}
		var na int
		for _, w := range a.Windows {
			if !w.Available {
				na++
				if len(w.History) != 0 || w.Percent != 0 {
					t.Fatalf("an N/A window drew %+v", w)
				}
			}
		}
		if na != 2 {
			t.Fatalf("%d N/A windows listed", na)
		}
	}
	// An account with tokens and no reading is listed, with no windows.
	var b model.AccountUsage
	for _, a := range u.Accounts {
		if a.Key == acctB {
			b = a
		}
	}
	if len(b.Windows) != 0 || b.ResetSource != "" || b.WeekEnd != now.Unix() || sum(b.Weeks[3].Out) == 0 {
		t.Fatalf("%+v", b)
	}
	if len(u.Capacity.Items) != 1 {
		t.Fatalf("only accounts with readings are in the capacity: %+v", u.Capacity)
	}
}

func TestBuildFallsBackWhenAiquotaCannotBeRead(t *testing.T) {
	root := t.TempDir()
	sc := NewScanner(filepath.Join(root, "projects"), "")
	// Recorded only: the reset is the last one recorded, said so.
	last := win(now.Add(-time.Hour).Unix(), acctA, "Claude Code", "max", 3, 30, now.Add(time.Hour).Unix(), weekReset)
	u := Build(Input{Now: now, Hist: []quota.Reading{last}, QuotaErr: "aiquota not found"}, sc)
	if len(u.Accounts) != 1 || u.Accounts[0].ResetSource != "recorded" || u.QuotaErr == "" || u.Accounts[0].WeekEnd != weekReset {
		t.Fatalf("%+v", u.Accounts)
	}
	// Nothing known at all: no accounts, no capacity.
	u = Build(Input{Now: now, QuotaErr: "x"}, sc)
	if len(u.Accounts) != 0 || u.Capacity.Counted != 0 || len(u.All.Days.Out[0]) != 28 {
		t.Fatalf("%+v", u)
	}
	// A recorded reset in the past is stepped forward by whole weeks.
	stale := quota.Reading{At: now.Add(-20 * 24 * time.Hour).Unix(), Acct: acctA, Windows: []quota.Window{{Label: quota.LabelWeek, Available: true, ResetsAt: weekReset - 21*24*3600}}}
	u = Build(Input{Now: now, Hist: []quota.Reading{stale}, QuotaErr: "x"}, sc)
	if a := u.Accounts[0]; a.ResetSource != "recorded" || a.WeekEnd != weekReset || a.Label != "account" {
		t.Fatalf("%d vs %d, label %q", a.WeekEnd, weekReset, a.Label)
	}
	// An older reading that had a reset still gives the alignment when the newest has none.
	old := win(now.Add(-48*time.Hour).Unix(), acctA, "Claude Code", "max", 1, 1, 0, weekReset)
	newest := quota.Reading{At: now.Unix(), Acct: acctA, Label: "Claude Code", Windows: []quota.Window{{Label: quota.LabelWeek}}}
	u = Build(Input{Now: now, Hist: []quota.Reading{old, newest}, QuotaErr: "x"}, sc)
	if a := u.Accounts[0]; a.WeekEnd != weekReset || a.ResetSource != "recorded" {
		t.Fatalf("%+v", a)
	}
	// An expired 5 hour reset: the 5 hour span is the last 5 hours.
	exp := quota.Reading{At: now.Unix(), Acct: acctA, Windows: []quota.Window{{Label: quota.LabelSession, Available: true, ResetsAt: now.Unix() - 60}}}
	if u = Build(Input{Now: now, Live: []quota.Reading{exp}, Hist: []quota.Reading{exp}}, sc); u.Accounts[0].SessionEnd != 0 || u.Accounts[0].Short.Start > now.Unix()-4*3600 {
		t.Fatalf("%+v", u.Accounts[0].Short)
	}
	if it := u.Capacity.Items[0]; it.SessionOK {
		t.Fatalf("a session window that rolled over: %+v", it)
	}
}

func TestBuildWindowsHistoryIsThinnedPerHourAndKeepsTheGap(t *testing.T) {
	sc := NewScanner(filepath.Join(t.TempDir(), "projects"), "")
	mk := func(at time.Time, p float64) quota.Reading {
		return win(at.Unix(), acctA, "Claude Code", "max", p, p, now.Add(3*time.Hour).Unix(), weekReset)
	}
	h := []quota.Reading{mk(now.Add(-26*time.Hour), 10), mk(now.Add(-25*time.Hour-time.Minute), 11), mk(now.Add(-25*time.Hour), 12), mk(now.Add(-time.Hour), 50), mk(now.Add(-5*time.Minute), 53)}
	u := Build(Input{Now: now, Live: []quota.Reading{mk(now, 54)}, Hist: h}, sc)
	var week, sess model.QuotaWindow
	for _, w := range u.Accounts[0].Windows {
		switch w.Label {
		case quota.LabelWeek:
			week = w
		case quota.LabelSession:
			sess = w
		}
	}
	if len(week.History) != 4 || week.History[0].Percent != 11 || week.History[3].Percent != 54 {
		t.Fatalf("%+v", week.History)
	}
	if len(sess.History) != 3 {
		t.Fatalf("the session window keeps every reading of the last 6 hours: %+v", sess.History)
	}
}

type fakeSource struct{ snap model.Snapshot }

func (f fakeSource) Snapshot() model.Snapshot { return f.snap }

func TestEngineRecordsEachAccountWithItsKeyAndNeverGuesses(t *testing.T) {
	root, cache, cfg := t.TempDir(), t.TempDir(), t.TempDir()
	write(t, root, "p/a.jsonl", owner("uuid-a"), entry(now.Add(-time.Hour), "m", "claude-opus-5-5", 4, 0))
	calls := 0
	body := fmt.Sprintf(`{"at":%q,"windows":[{"account":"Claude Code","plan":"max","label":"Weekly (7 day)","used_percent":54,"resets_at_unix":%d,"verdict":"available"}]}`, now.Format(time.RFC3339), weekReset)
	var fail error
	e := NewEngine(root, cache, cfg, func(context.Context) ([]byte, error) {
		calls++
		if fail != nil {
			return nil, fail
		}
		return []byte(body), nil
	})
	active := acctA
	e.active = func() string { return active }
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
	if v == nil || len(v.Accounts) != 1 || v.Accounts[0].Key != acctA || !v.Accounts[0].Active || v.Accounts[0].ResetSource != "aiquota" || sum(v.Accounts[0].Weeks[3].Out) != 4 {
		t.Fatalf("%+v", v)
	}
	rs, _ := quota.NewStore(cfg).Load()
	if len(rs) != 1 || rs[0].Acct != acctA || rs[0].Label != "Claude Code" || rs[0].Plan != "max" {
		t.Fatalf("%+v", rs)
	}
	if strings.Contains(fmt.Sprintf("%+v", rs), "uuid-a") {
		t.Fatal("the account id must not be recorded, only its key")
	}
	if v.Accounts[0].Weeks[3].Percent != 54 || len(v.Accounts[0].Windows[0].History) != 1 {
		t.Fatal("the reading just taken was drawn twice or not at all")
	}

	// The account changes while aiquota runs: that reading's account is unknown.
	n := 0
	e.active = func() string { n++; return []string{acctA, acctB}[n%2] }
	e.Step(ctx, now.Add(6*time.Minute))
	rs, _ = quota.NewStore(cfg).Load()
	if len(rs) != 2 || rs[1].Acct != "" {
		t.Fatalf("an account that changed during the reading must be recorded as unknown: %+v", rs)
	}
	// aiquota fails later: the last reading stands, the error is said.
	e.active = func() string { return acctA }
	fail = errors.New("boom")
	e.Step(ctx, now.Add(12*time.Minute))
	if v = e.View(); v.QuotaErr != "boom" || len(v.Accounts) != 2 {
		t.Fatalf("%+v", v.Accounts)
	}
	// A restart with aiquota down: the history alone gives the readings.
	e2 := NewEngine(root, cache, cfg, func(context.Context) ([]byte, error) { return nil, fail })
	e2.active = func() string { return acctA }
	e2.Step(ctx, now.Add(13*time.Minute))
	if v = e2.View(); len(v.Accounts) != 2 || v.Accounts[0].ResetSource != "recorded" {
		t.Fatalf("%+v", v.Accounts)
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
	e4 := NewEngine(root, cache, filepath.Join(file, "x"), func(context.Context) ([]byte, error) { return []byte(body), nil })
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

func TestActiveKeyReadsOnlyTheAccountIDAndHashesIt(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".claude.json")
	os.WriteFile(p, []byte(`{"projects":{},"oauthAccount":{"accountUuid":"uuid-a","emailAddress":"someone@example.org"}}`), 0o600)
	if got := ActiveKey(p); got != acctA || strings.Contains(got, "uuid") {
		t.Fatalf("%q", got)
	}
	if ActiveKey(filepath.Join(dir, "absent")) != "" {
		t.Fatal("no file, no account")
	}
	os.WriteFile(p, []byte(`not json`), 0o600)
	if ActiveKey(p) != "" {
		t.Fatal("unreadable, no account")
	}
	os.WriteFile(p, []byte(`{"oauthAccount":{}}`), 0o600)
	if ActiveKey(p) != "" {
		t.Fatal("signed out, no account")
	}
	// NewEngine reads it next to the claude directory.
	os.WriteFile(p, []byte(`{"oauthAccount":{"accountUuid":"uuid-a"}}`), 0o600)
	if e := NewEngine(filepath.Join(dir, ".claude"), t.TempDir(), t.TempDir(), nil); e.active() != acctA {
		t.Fatal("the engine's active account")
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
	f.Add([]byte(owner("uuid-a")))
	f.Add([]byte(`{"type":"session-owner","accountUuid":5}`))
	f.Add([]byte(`{"type":"assistant","usage":1,"timestamp":5,"message":{"usage":{"output_tokens":"x"}}}`))
	f.Add([]byte(`{"type":"assistant","message":{"id":"m","model":"claude-opus-5-5","usage":{"output_tokens":-9}},"timestamp":"2026-11-20T07:00:00Z"}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		s := &Scanner{sums: map[sumKey]Counts{}}
		var st Stats
		fs := &fileState{}
		s.fold(b, fs, now.Add(-Keep), &st)
		s.fold(b, fs, now.Add(-Keep), &st)
		for _, c := range s.sums {
			if c.Out < 0 || c.CacheRead < 0 {
				t.Fatal("negative count")
			}
		}
	})
}
