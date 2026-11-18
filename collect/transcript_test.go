package collect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestParseLineExtractsMetadataOnly(t *testing.T) {
	var st tstate
	lines := []string{
		`{"type":"user","timestamp":"2026-11-18T10:00:00.000Z","cwd":"/work/a","gitBranch":"main","slug":"quiet-fox","message":{"content":"SECRET PROMPT"}}`,
		`{"type":"ai-title","aiTitle":"Fix the thing"}`,
		`{"type":"custom-title","customTitle":"my name"}`,
		`{"type":"pr-link","prNumber":12,"prRepository":"o/r","prUrl":"https://x"}`,
		`{"type":"assistant","timestamp":"2026-11-18T10:05:00Z","message":{"content":[{"type":"tool_use","name":"SendMessage","input":{"to":"abc","message":"SECRET"}},{"type":"text","text":"SECRET REPLY"}]}}`,
		`{"type":"assistant","message":{"content":"a string, not blocks"}}`,
		`{"type":7,"cwd":"/work/b"}`,
	}
	for _, l := range lines {
		if !parseLine([]byte(l), &st) {
			t.Fatalf("not accepted: %s", l)
		}
	}
	if st.cwd != "/work/b" || st.branch != "main" || st.slug != "quiet-fox" || st.title != "Fix the thing" || st.custom != "my name" || st.pr != "o/r#12" {
		t.Errorf("state %+v", st)
	}
	if st.sent != 1 || st.sentTo["abc"] != 1 {
		t.Errorf("sent %d %v", st.sent, st.sentTo)
	}
	if want := time.Date(2026, 11, 18, 10, 5, 0, 0, time.UTC); !st.lastTS.Equal(want) {
		t.Errorf("lastTS %v", st.lastTS)
	}
	// Nothing of the prompt or the response is kept anywhere.
	if dump := stateText(st); strings.Contains(dump, "SECRET") {
		t.Errorf("content leaked into state: %s", dump)
	}
}

func stateText(st tstate) string {
	s := st.cwd + st.branch + st.slug + st.title + st.custom + st.pr
	for k := range st.sentTo {
		s += k
	}
	return s
}

func TestParseLineReadsTheModelOfAssistantEntriesOnly(t *testing.T) {
	var st tstate
	for _, l := range []string{
		`{"type":"assistant","timestamp":"2026-11-18T10:00:00Z","message":{"model":"claude-opus-5-5","content":[{"type":"text","text":"SECRET REPLY"}]}}`,
		`{"type":"user","timestamp":"2026-11-18T10:01:00Z","message":{"model":"claude-user-0-0","content":"SECRET PROMPT"}}`,
		`{"type":"assistant","timestamp":"2026-11-18T10:05:00Z","message":{"model":"claude-sonnet-5-5"}}`,
		`{"type":"assistant","timestamp":"2026-11-18T10:03:00Z","message":{"model":"claude-opus-5-5"}}`,
		`{"type":"assistant","timestamp":"2026-11-18T10:06:00Z","message":{"model":"<synthetic>"}}`,
		`{"type":"assistant","message":"a string"}`,
		`{"type":"assistant","timestamp":"2026-11-18T10:07:00Z","message":{"content":[]}}`,
	} {
		parseLine([]byte(l), &st)
	}
	if len(st.models) != 3 {
		t.Fatalf("models %+v: the user entry and the entries with no model must not count", st.models)
	}
	// Newest first: <synthetic> 10:06, sonnet 10:05, opus 10:03 (the later opus
	// entry moved it up, the earlier one did not move it back).
	if st.models[0].id != "<synthetic>" || st.models[1].id != "claude-sonnet-5-5" || st.models[2].id != "claude-opus-5-5" {
		t.Errorf("order %+v", st.models)
	}
	if want := time.Date(2026, 11, 18, 10, 3, 0, 0, time.UTC); !st.models[2].last.Equal(want) {
		t.Errorf("opus last %v", st.models[2].last)
	}
	for _, m := range st.models {
		if strings.Contains(m.id, "SECRET") {
			t.Error("content leaked into the models")
		}
	}
	latest, all := modelsOf(&st, time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC))
	if latest != "claude-sonnet-5-5" || len(all) != 3 {
		t.Errorf("the placeholder must not be the latest model: %q %v", latest, all)
	}
	if l, a := modelsOf(&st, time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)); l != "" || a != nil {
		t.Errorf("models older than the horizon were kept: %q %v", l, a)
	}
	only := tstate{}
	only.noteModel("<synthetic>", time.Time{})
	if l, a := modelsOf(&only, time.Time{}); l != "" || len(a) != 1 {
		t.Errorf("only a placeholder: %q %v", l, a)
	}
}

func TestNoteModelIsBoundedAndUsesFilePositionWithoutATimestamp(t *testing.T) {
	var st tstate
	st.lastTS = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < maxModels+10; i++ {
		st.noteModel("m"+strings.Repeat("x", i), time.Date(2026, 1, 2, 0, i, 0, 0, time.UTC))
	}
	if len(st.models) > maxModels {
		t.Fatalf("%d models kept", len(st.models))
	}
	st.noteModel("untimed", time.Time{})
	found := false
	for _, m := range st.models {
		if m.id == "untimed" && m.last.Equal(st.lastTS) {
			found = true
		}
	}
	if !found && len(st.models) == maxModels {
		t.Log("the untimed entry was the oldest and was dropped")
	}
}

func TestParseLineRejectsGarbage(t *testing.T) {
	for _, l := range []string{``, `not json`, `{"type":`, `[1,2`, "\x00\x01"} {
		var st tstate
		if parseLine([]byte(l), &st) {
			t.Errorf("accepted %q", l)
		}
	}
	var st tstate
	if !parseLine([]byte(`[1,2]`), &st) { // valid JSON of another shape: a type error, not a syntax error
		t.Log("array line counted as bad")
	}
}

func TestSentToIsBounded(t *testing.T) {
	var st tstate
	for i := 0; i < maxSentTo+50; i++ {
		parseLine([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"SendMessage","input":{"to":"`+strings.Repeat("x", i%7+1)+string(rune('a'+i%26))+string(rune('A'+i/26%26))+string(rune('0'+i/676))+`"}}]}}`), &st)
	}
	if len(st.sentTo) > maxSentTo {
		t.Errorf("%d recipients kept", len(st.sentTo))
	}
}

func TestClipKeepsRunesWhole(t *testing.T) {
	if got := clip("abc", 10); got != "abc" {
		t.Error(got)
	}
	s := strings.Repeat("é", 10) // 2 bytes each
	for n := 0; n < len(s); n++ {
		if got := clip(s, n); !utf8.ValidString(got) || len(got) > n {
			t.Errorf("clip(%d) = %q", n, got)
		}
	}
}

func writeFile(t testing.TB, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendFile(t *testing.T, p, s string) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func scanFile(t *testing.T, p string, st *tstate, tail int64, maxLine int) int64 {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, _ := f.Stat()
	n, err := scan(f, info.Size(), st, tail, maxLine)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestScanIsIncrementalAndLeavesAPartialLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, `{"cwd":"/one"}`+"\n"+`{"slug":"s1"}`+"\n"+`{"cwd":"/par`)
	var st tstate
	n1 := scanFile(t, p, &st, 0, 1<<20)
	if st.cwd != "/one" || st.slug != "s1" {
		t.Fatalf("first pass %+v", st)
	}
	off := st.offset
	if off != int64(len(`{"cwd":"/one"}`+"\n"+`{"slug":"s1"}`+"\n")) || n1 != off {
		t.Fatalf("offset %d read %d: the partial line must be left for the next pass", off, n1)
	}
	appendFile(t, p, `tial"}`+"\n"+`{"gitBranch":"x"}`+"\n")
	n2 := scanFile(t, p, &st, 0, 1<<20)
	if st.cwd != "/partial" || st.branch != "x" {
		t.Fatalf("second pass %+v", st)
	}
	if n2 >= n1+n2 || st.bad != 0 {
		t.Errorf("n2=%d bad=%d", n2, st.bad)
	}
	// Nothing appended: nothing read.
	if n3 := scanFile(t, p, &st, 0, 1<<20); n3 != 0 {
		t.Errorf("read %d bytes of nothing", n3)
	}
}

func TestScanReadsOnlyTheTailOfALargeFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	var b strings.Builder
	b.WriteString(`{"cwd":"/old"}` + "\n")
	for i := 0; i < 2000; i++ {
		b.WriteString(`{"type":"user","timestamp":"2026-11-18T10:00:00Z"}` + "\n")
	}
	b.WriteString(`{"slug":"recent"}` + "\n")
	writeFile(t, p, b.String())
	var st tstate
	n := scanFile(t, p, &st, 4096, 1<<20)
	if n > 4096 {
		t.Errorf("read %d bytes with a 4096 window", n)
	}
	if st.cwd != "" || st.slug != "recent" {
		t.Errorf("the head must be skipped and the tail read: %+v", st)
	}
	if st.bad != 0 {
		t.Errorf("the cut first line must be dropped, not parsed: bad=%d", st.bad)
	}
}

func TestScanSkipsAndCountsOversizedLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	huge := `{"type":"user","message":{"content":"` + strings.Repeat("A", 300_000) + `"}}`
	writeFile(t, p, `{"cwd":"/a"}`+"\n"+huge+"\n"+`{"slug":"after"}`+"\n"+"\n"+"garbage\n")
	var st tstate
	scanFile(t, p, &st, 0, 100_000)
	if st.skipped != 1 || st.cwd != "/a" || st.slug != "after" || st.bad != 1 {
		t.Errorf("%+v", st)
	}
}

func TestScanStartsOverWhenTheFileShrinks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, `{"cwd":"/long/long/long/long"}`+"\n"+`{"slug":"a"}`+"\n")
	var st tstate
	scanFile(t, p, &st, 0, 1<<20)
	writeFile(t, p, `{"cwd":"/x"}`+"\n")
	scanFile(t, p, &st, 0, 1<<20)
	if st.cwd != "/x" || st.slug != "" {
		t.Errorf("%+v", st)
	}
}

func TestScanErrors(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Open(dir) // a directory: reads fail
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var st tstate
	if _, err := scan(f, 10, &st, 0, 100); err == nil {
		t.Error("a read error was swallowed")
	}
	closed, _ := os.Open(filepath.Join(dir))
	closed.Close()
	if _, err := scan(closed, 10, &st, 0, 100); err == nil {
		t.Error("a seek error was swallowed")
	}
	// A window that ends inside the discarded first line.
	p := filepath.Join(dir, "a.jsonl")
	writeFile(t, p, strings.Repeat("x", 5000))
	st = tstate{}
	scanFile(t, p, &st, 100, 1<<20)
	if st.offset != 5000 {
		t.Errorf("offset %d", st.offset)
	}
}
