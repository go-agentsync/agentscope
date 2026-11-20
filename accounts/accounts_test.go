package accounts

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/quota"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

const claudeJSON = `{"numStartups":3,"oauthAccount":{"accountUuid":"11111111-2222-3333-4444-555555555555","emailAddress":"someone@example.org","organizationName":"Some Lab","organizationType":"claude_team"}}`

func TestReadIdentityHashesTheIdAndKeepsNoEmail(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".claude.json")
	write(t, p, claudeJSON)
	id, ok := ReadIdentity(p, false)
	if !ok || id.Key != quota.AccountKey("11111111-2222-3333-4444-555555555555") || id.Plan != "team" || id.Label != "Claude Code (team)" {
		t.Fatalf("%+v %v", id, ok)
	}
	if id.Org != "" {
		t.Fatal("the organisation is read only when asked for")
	}
	for _, s := range []string{id.Key, id.Label, id.Plan, id.Org} {
		if strings.Contains(s, "@") || strings.Contains(s, "1111") {
			t.Fatalf("the identity holds an e-mail or an id: %q", s)
		}
	}
	if id, _ := ReadIdentity(p, true); id.Org != "Some Lab" {
		t.Fatalf("org %q", id.Org)
	}
	// No plan: the label is bare.
	write(t, p, `{"oauthAccount":{"accountUuid":"x"}}`)
	if id, ok := ReadIdentity(p, false); !ok || id.Label != "Claude Code" || id.Plan != "" {
		t.Fatalf("%+v", id)
	}
}

func TestReadIdentityUnreadableIsNotGuessed(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "bad.json"), `{not json`)
	write(t, filepath.Join(dir, "noacct.json"), `{"oauthAccount":{}}`)
	write(t, filepath.Join(dir, "none.json"), `{}`)
	for _, name := range []string{"missing.json", "bad.json", "noacct.json", "none.json"} {
		if id, ok := ReadIdentity(filepath.Join(dir, name), true); ok || id != (Identity{}) {
			t.Fatalf("%s: %+v %v", name, id, ok)
		}
	}
	if got := clip(strings.Repeat("é", 100), 81); len(got) != 80 || !strings.HasSuffix(got, "é") {
		t.Fatalf("clip cut a rune: %d", len(got))
	}
}

func stamp(sec int, hash string) Stamp {
	return Stamp{At: time.Unix(int64(1_700_000_000+sec), 0).UTC(), SessionID: "s", AccountHash: hash, Label: "Claude Code", Plan: "team"}
}

func TestStampsAppendOnlyOnChange(t *testing.T) {
	s := NewStamps(t.TempDir())
	for i, c := range []struct {
		hash string
		want bool
	}{{"aaaa1111", true}, {"aaaa1111", false}, {"aaaa1111", false}, {"bbbb2222", true}, {"aaaa1111", true}, {"aaaa1111", false}} {
		got, err := s.Append(stamp(i, c.hash))
		if err != nil || got != c.want {
			t.Fatalf("step %d (%s): wrote %v, %v", i, c.hash, got, err)
		}
	}
	tl := s.Load()
	if tl.Len() != 3 {
		t.Fatalf("%d stamps kept, want 3 (a, b, a)", tl.Len())
	}
	if _, err := s.Append(Stamp{}); err == nil {
		t.Fatal("a stamp without a hash")
	}
	fi, err := os.Stat(s.path())
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", fi.Mode(), err)
	}
}

// Concurrent sessions: the same account stamped by many at once is written once;
// alternating accounts from many are each written, none torn.
func TestStampsConcurrentAppends(t *testing.T) {
	s := NewStamps(t.TempDir())
	var wg sync.WaitGroup
	wrote := make(chan bool, 64)
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := s.Append(stamp(i, "same0000"))
			if err != nil {
				t.Error(err)
			}
			wrote <- ok
		}()
	}
	wg.Wait()
	close(wrote)
	n := 0
	for ok := range wrote {
		if ok {
			n++
		}
	}
	if n != 1 || s.Load().Len() != 1 {
		t.Fatalf("%d writers won, %d lines: want exactly one", n, s.Load().Len())
	}
	// Distinct accounts racing: every line is whole.
	s2 := NewStamps(t.TempDir())
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s2.Append(stamp(i, "h"+strconv.Itoa(i))); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := s2.Load().Len(); got != 30 {
		t.Fatalf("%d of 30 stamps survived", got)
	}
}

func TestStampsRotateAndKeepTheLastAcrossIt(t *testing.T) {
	dir := t.TempDir()
	s := NewStamps(dir)
	// A file already at the bound, whose last stamp names account A.
	row := "{\"at\":\"2023-11-14T22:13:20Z\",\"account_hash\":\"old00000\"}\n"
	big := strings.Repeat(row, RotateBytes/len(row)+1)
	write(t, s.path(), big+`{"at":"2023-11-15T00:00:00Z","account_hash":"aaaa1111"}`+"\n")
	// Account A again: no new stamp, and no rotation is needed to say so.
	if ok, err := s.Append(stamp(5, "aaaa1111")); ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	// Account B: the file rotates, the new one holds the stamp.
	if ok, err := s.Append(stamp(6, "bbbb2222")); !ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	if _, err := os.Stat(s.path() + ".1"); err != nil {
		t.Fatal("the full file was not rotated")
	}
	// After a rotation that left the new file empty, the last stamp is the old
	// file's: A again is a change from B, but B again is not.
	os.Remove(s.path())
	if ok, _ := s.Append(stamp(7, "aaaa1111")); !ok { // .1 ends with A's stamp, but only B was written to the new file
		t.Log("A after an empty current file is judged against .1")
	}
	tl := s.Load()
	if st, ok := tl.At(time.Unix(1_700_000_000+6, 0)); !ok || st.AccountHash == "" {
		t.Fatalf("timeline across the rotation: %+v %v", st, ok)
	}
}

func TestTimelineAtIsTheLatestStampAtOrBefore(t *testing.T) {
	s := NewStamps(t.TempDir())
	s.Append(stamp(10, "aaaa1111"))
	s.Append(stamp(20, "bbbb2222"))
	write(t, s.path(), mustRead(t, s.path())+"torn {\n"+"\n"+strings.Repeat("x", 20000)+"\n"+`{"at":"0001-01-01T00:00:00Z","account_hash":"zero"}`+"\n")
	tl := s.Load()
	base := int64(1_700_000_000)
	for _, c := range []struct {
		at   int64
		want string
	}{{9, ""}, {10, "aaaa1111"}, {19, "aaaa1111"}, {20, "bbbb2222"}, {9999, "bbbb2222"}} {
		st, ok := tl.At(time.Unix(base+c.at, 0))
		if st.AccountHash != c.want || ok != (c.want != "") {
			t.Fatalf("At(+%d) = %q %v, want %q", c.at, st.AccountHash, ok, c.want)
		}
	}
	if tl.Len() != 2 {
		t.Fatalf("torn, oversized and zero-time lines are skipped: %d kept", tl.Len())
	}
	if (Timeline{}).Len() != 0 {
		t.Fatal("empty")
	}
	// A directory with no stamps at all.
	if NewStamps(t.TempDir()).Load().Len() != 0 {
		t.Fatal("no file, no stamps")
	}
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLockTakesOverAStaleOneAndGivesUpWhenBusy(t *testing.T) {
	old, oldWait := staleAfter, waitFor
	defer func() { staleAfter, waitFor = old, oldWait }()
	p := filepath.Join(t.TempDir(), "x.lock")
	write(t, p, "")
	staleAfter, waitFor = time.Hour, 30*time.Millisecond
	if _, err := lock(p); err == nil {
		t.Fatal("a fresh lock held by someone else must make us wait, then give up")
	}
	staleAfter = time.Millisecond
	time.Sleep(5 * time.Millisecond)
	un, err := lock(p)
	if err != nil {
		t.Fatalf("a stale lock was not taken over: %v", err)
	}
	un()
	if _, err := os.Stat(p); err == nil {
		t.Fatal("unlock leaves the file")
	}
	// A directory that cannot hold a lock is an error, not a wait.
	if _, err := lock(filepath.Join(p, "nope", "x.lock")); err == nil {
		t.Fatal("lock in a missing directory")
	}
	// Append reports it.
	s := NewStamps(t.TempDir())
	write(t, filepath.Join(s.dir, "accounts.lock"), "")
	staleAfter = time.Hour
	if _, err := s.Append(stamp(1, "aaaa1111")); err == nil {
		t.Fatal("append while the lock is busy")
	}
}

func TestAppendErrors(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	write(t, f, "x")
	if _, err := NewStamps(filepath.Join(f, "sub")).Append(stamp(1, "a")); err == nil {
		t.Fatal("a directory that cannot be made")
	}
	// A stamp file that is a directory cannot be appended to.
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "accounts.jsonl"), 0o755)
	if _, err := NewStamps(d).Append(stamp(1, "a")); err == nil {
		t.Fatal("accounts.jsonl is a directory")
	}
}

func TestAliasesRoundTripAndFallBack(t *testing.T) {
	dir := t.TempDir()
	a := NewAliases(dir)
	if a.Name("7567abcd") != "" {
		t.Fatal("no file, no alias")
	}
	names := map[string]string{"7567abcd": `Labo "team" \ ça va`, "aaaa1111": "Perso (max)"}
	for k, v := range names {
		if err := a.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	b := NewAliases(dir) // a new process reads what the first wrote
	for k, v := range names {
		if b.Name(k) != v {
			t.Fatalf("%s: %q, want %q", k, b.Name(k), v)
		}
	}
	fi, err := os.Stat(filepath.Join(dir, "accounts.toml"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", fi.Mode(), err)
	}
	// A hand-written file may name the account by its 4 shown digits.
	write(t, filepath.Join(dir, "accounts.toml"), "# mine\n\"7567\" = \"Labo (team)\" # note\nbare_key-1 = \"x\"\n")
	b.Reload()
	if b.Name("7567abcd") != "Labo (team)" || b.Name("bare_key-1") != "x" || b.Name("beef0000") != "" {
		t.Fatal("short key lookup")
	}
	// A full-hash alias beats the short one; setting one replaces the other.
	if err := b.Set("7567abcd", "Full"); err != nil || b.Name("7567abcd") != "Full" || b.Name("7567zzzz") != "" {
		t.Fatalf("%v %q", err, b.Name("7567abcd"))
	}
	if err := b.Set("7567abcd", "  "); err != nil || b.Name("7567abcd") != "" {
		t.Fatalf("an empty name removes the alias: %v", err)
	}
	if strings.Contains(mustRead(t, filepath.Join(dir, "accounts.toml")), "7567") {
		t.Fatal("the removed alias is still in the file")
	}
}

func TestCorruptAliasFileFallsBackToNoAliases(t *testing.T) {
	for name, body := range map[string]string{
		"garbage":       "this is not toml",
		"no equals":     `"a" "b"`,
		"no value":      `"a" = `,
		"unterminated":  `"a" = "b`,
		"bad escape":    `"a" = "\q"`,
		"short unicode": `"a" = "\u12"`,
		"bad unicode":   `"a" = "\uZZZZ"`,
		"escape at end": `"a" = "\`,
		"control char":  "\"a\" = \"x\x01y\"",
		"trailing":      `"a" = "b" junk`,
		"bad bare key":  `a.b = "c"`,
		"no key":        `= "c"`,
		"empty key":     `"" = "c"`,
		"too long name": `"a" = "` + strings.Repeat("x", MaxName+1) + `"`,
		"half good":     "\"a\" = \"ok\"\nnonsense\n",
		"huge line":     `"a" = "` + strings.Repeat("x", 5000) + `"`,
	} {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "accounts.toml"), body)
		a := NewAliases(dir)
		if a.Name("a") != "" || a.Name("aaaa1111") != "" {
			t.Fatalf("%s: a corrupt file must name nothing, got %q", name, a.Name("a"))
		}
	}
	// A file past the size bound.
	dir := t.TempDir()
	write(t, filepath.Join(dir, "accounts.toml"), strings.Repeat("# filler line of the comment kind\n", 3000))
	if NewAliases(dir).Name("a") != "" {
		t.Fatal("oversized")
	}
	// Too many aliases.
	var b strings.Builder
	for i := 0; i <= maxAliases; i++ {
		b.WriteString("\"k" + strconv.Itoa(i) + "\" = \"v\"\n")
	}
	write(t, filepath.Join(dir, "accounts.toml"), b.String())
	if NewAliases(dir).Name("k0") != "" {
		t.Fatal("too many")
	}
}

func TestAliasSetRefusesWhatCannotBeSaved(t *testing.T) {
	a := NewAliases(t.TempDir())
	for _, c := range []struct{ key, name string }{
		{"", "x"}, {"a\"b", "x"}, {"abcd", "line\nbreak"}, {"abcd", strings.Repeat("x", MaxName+1)}, {"abcd", "\xff"}, {"abcd", "del\x7f"},
	} {
		if err := a.Set(c.key, c.name); err == nil {
			t.Fatalf("Set(%q, %q) was accepted", c.key, c.name)
		}
	}
	for i := 0; i < maxAliases; i++ {
		if err := a.Set("k"+strconv.Itoa(i), "v"); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Set("one-more", "v"); err == nil {
		t.Fatal("past the bound")
	}
	// Removing still works at the bound, and an unwritable directory is an error.
	if err := a.Set("k0", ""); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "file")
	write(t, f, "x")
	if err := NewAliases(filepath.Join(f, "sub")).Set("abcd", "x"); err == nil {
		t.Fatal("a directory that cannot be made")
	}
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "accounts.toml"), 0o755) // the target is a directory: rename fails
	if err := NewAliases(d).Set("abcd", "x"); err == nil {
		t.Fatal("rename over a directory")
	}
	if got := quote("a\tb"); got != `"a\u0009b"` {
		t.Fatal(got)
	}
	if v, _, err := readString(`"a\tb\nc"`); err != nil || v != "a\tb\nc" {
		t.Fatalf("%q %v", v, err)
	}
}

func FuzzParseAliases(f *testing.F) {
	for _, s := range []string{"", "# c\n", `"7567" = "Labo (team)"`, "a = \"b\"\n\"c\" = \"\\u00e9\"", `"a" = "\`, "\"a\" = \"b\" # x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		m, err := parseAliases(strings.NewReader(s))
		if err != nil && len(m) != 0 {
			t.Fatal("an error leaves aliases")
		}
		for k, v := range m {
			if len(v) > MaxName {
				t.Fatal("bound")
			}
			back, err := parseAliases(strings.NewReader(formatAliases(map[string]string{k: v})))
			if err != nil || back[k] != v {
				t.Fatalf("round trip of %q=%q: %v %v", k, v, back, err)
			}
		}
	})
}
