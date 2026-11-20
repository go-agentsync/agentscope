package quota

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `{"version":1,"at":"2026-11-20T07:12:08.01871Z","windows":[
 {"label":"Session (5hr)","used_percent":2,"resets_at_unix":1795176600,"verdict":"available"},
 {"label":"Weekly (7 day)","used_percent":54,"resets_at_unix":1795183200,"verdict":"available"},
 {"label":"Weekly Sonnet","used_percent":0,"resets_at_unix":0,"verdict":"n/a"},
 {"label":"Weekly Fable","used_percent":150,"resets_at_unix":1795183200,"verdict":"available"}]}`

func TestParseKeepsNotAvailableWindowsOutOfTheNumbers(t *testing.T) {
	r, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Windows) != 4 || r.At == 0 {
		t.Fatalf("%+v", r)
	}
	if w, _ := r.Find(LabelWeek); !w.Available || w.Percent != 54 || w.ResetsAt != 1795183200 {
		t.Fatalf("%+v", w)
	}
	if w, ok := r.Find("Weekly Sonnet"); !ok || w.Available || w.Percent != 0 || w.ResetsAt != 0 {
		t.Fatalf("n/a window %+v", w)
	}
	if w, _ := r.Find("Weekly Fable"); w.Percent != 100 {
		t.Fatalf("percent not clamped: %+v", w)
	}
	if _, ok := r.Find("nope"); ok {
		t.Fatal("found a window that is not there")
	}
}

func TestParseRefusesWhatIsNotAReading(t *testing.T) {
	for _, in := range []string{``, `{`, `{"windows":[{"label":"x","verdict":"available"}]}`, `{"at":"2026-11-20T07:12:08Z","windows":[]}`} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("Parse(%q) accepted", in)
		}
	}
	var many strings.Builder
	many.WriteString(`{"at":"2026-11-20T07:12:08Z","windows":[`)
	for i := 0; i < 40; i++ {
		if i > 0 {
			many.WriteString(",")
		}
		many.WriteString(`{"label":"w","verdict":"available","used_percent":-5}`)
	}
	many.WriteString(`]}`)
	r, err := Parse([]byte(many.String()))
	if err != nil || len(r.Windows) != maxWindows || r.Windows[0].Percent != 0 {
		t.Fatalf("%d windows, %v", len(r.Windows), err)
	}
	if clamp(clampNaN()) != 0 {
		t.Fatal("NaN not clamped")
	}
	if len(clip(strings.Repeat("x", 100), 64)) != 64 {
		t.Fatal("clip")
	}
}

func clampNaN() float64 { var z float64; return z / z }

func TestStoreAppendsLoadsAndRotates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agentscope")
	s := NewStore(dir)
	if rs, bad := s.Load(); len(rs) != 0 || bad != 0 {
		t.Fatal("an empty store has readings")
	}
	// A gap in the history, and a window that is not available in one reading.
	for _, r := range []Reading{
		{At: 1000, Windows: []Window{{Label: LabelWeek, Percent: 10, ResetsAt: 5000, Available: true}}},
		{At: 90000, Windows: []Window{{Label: LabelWeek, Percent: 20, ResetsAt: 5000, Available: true}, {Label: "Weekly Opus"}}},
	} {
		if err := s.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	rs, bad := s.Load()
	if bad != 0 || len(rs) != 2 || rs[1].At-rs[0].At != 89000 {
		t.Fatalf("%+v bad=%d", rs, bad)
	}
	if w, _ := rs[1].Find("Weekly Opus"); w.Available {
		t.Fatal("an N/A window came back available")
	}

	// Garbage lines are counted, not fatal; a line past the buffer too.
	f, _ := os.OpenFile(filepath.Join(dir, "quota.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("not json\n{\"at\":0}\n" + strings.Repeat("x", 100<<10) + "\n{\"at\":95000,\"w\":[]}\ntorn")
	f.Close()
	rs, bad = s.Load()
	if len(rs) != 3 || bad != 4 {
		t.Fatalf("%d readings, %d bad", len(rs), bad)
	}

	// Past RotateBytes the file is rotated; both halves are read, in order.
	if err := os.WriteFile(filepath.Join(dir, "quota.jsonl"), bytes.Repeat([]byte("{\"at\":1,\"w\":[]}\n"), RotateBytes/16+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Reading{At: 2e9}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "quota.jsonl.1")); err != nil {
		t.Fatal("not rotated:", err)
	}
	rs, _ = s.Load()
	if rs[len(rs)-1].At != 2e9 {
		t.Fatal("newest reading not last")
	}
}

func TestStoreAppendReportsWhatItCannotWrite(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f")
	os.WriteFile(file, nil, 0o600)
	if err := NewStore(filepath.Join(file, "sub")).Append(Reading{At: 1}); err == nil {
		t.Fatal("wrote under a file")
	}
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "quota.jsonl"), 0o700) // a directory where the file belongs
	if err := NewStore(dir).Append(Reading{At: 1}); err == nil {
		t.Fatal("opened a directory for append")
	}
	if err := NewStore(dir).Append(Reading{At: 1, Windows: []Window{{Percent: clampNaN()}}}); err == nil {
		t.Fatal("encoded NaN")
	}
}

func TestExecRunsAiquotaFromPathAndBoundsItsOutput(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1 $2 $3\" != \"status -o json\" ]; then exit 3; fi\n/bin/cat <<'EOF'\n" + sample + "\nEOF\n"
	if err := os.WriteFile(filepath.Join(dir, "aiquota"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	b, err := Exec(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(b); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "aiquota"), []byte("#!/bin/sh\nexit 1\n"), 0o700)
	if _, err := Exec(context.Background()); err == nil {
		t.Fatal("a failing aiquota is not an error")
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := Exec(context.Background()); err == nil {
		t.Fatal("no aiquota is not an error")
	}
	var out bytes.Buffer
	lw := &limitWriter{w: &out, left: 4}
	if n, _ := lw.Write([]byte("abcdefgh")); n != 8 || out.String() != "abcd" {
		t.Fatalf("%d %q", n, out.String())
	}
}
