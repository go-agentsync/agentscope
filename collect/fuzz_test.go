package collect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/go-agentsync/agentscope/model"
)

// The parsers read files written by other processes on this machine, one of
// which is a model driven by whatever a web page told it. Each one must take
// ANY bytes without panicking, without unbounded work, and without handing
// back something longer than its bound. Seeds live in testdata/fuzz/<name>/.

func FuzzParseLine(f *testing.F) {
	f.Add([]byte(`{"type":"user","timestamp":"2026-11-18T10:00:00Z","cwd":"/a","message":{"content":"x"}}`))
	f.Add([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"SendMessage","input":{"to":"a"}}]}}`))
	f.Add([]byte(`{"type":"ai-title","aiTitle":"t"}`))
	f.Add([]byte(`{"type":"pr-link","prNumber":-1,"prRepository":"o/r"}`))
	f.Add([]byte(`{"cwd":123,"type":[1]}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		var st tstate
		parseLine(b, &st)
		for _, s := range []string{st.cwd, st.title, st.custom, st.slug, st.branch, st.pr} {
			if len(s) > 600 {
				t.Fatalf("unbounded field of %d bytes", len(s))
			}
		}
		if len(st.models) > maxModels {
			t.Fatalf("unbounded models: %d", len(st.models))
		}
		for _, m := range st.models {
			if len(m.id) > 80 {
				t.Fatalf("unbounded model id of %d bytes", len(m.id))
			}
		}
		if len(st.sentTo) > maxSentTo+1 {
			t.Fatalf("unbounded recipients: %d", len(st.sentTo))
		}
	})
}

func FuzzScan(f *testing.F) {
	f.Add([]byte(`{"cwd":"/a"}`+"\n"+`{"slug":"b"}`+"\n"), uint8(0), uint8(200))
	f.Add([]byte("\n\n\r\n{"), uint8(3), uint8(10))
	f.Add([]byte(strings.Repeat("x", 3000)), uint8(40), uint8(60))
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, b []byte, tail, maxLine uint8) {
		p := filepath.Join(dir, "t.jsonl")
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		fh, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		defer fh.Close()
		var st tstate
		n, err := scan(fh, int64(len(b)), &st, int64(tail), int(maxLine)+1)
		if err != nil {
			t.Fatal(err)
		}
		if n > int64(len(b)) || st.offset > int64(len(b)) {
			t.Fatalf("read %d, offset %d of %d bytes", n, st.offset, len(b))
		}
		// A second pass over the same file reads nothing new.
		if n2, _ := scan(fh, int64(len(b)), &st, int64(tail), int(maxLine)+1); n2 != 0 {
			t.Fatalf("a second pass read %d bytes", n2)
		}
	})
}

func FuzzParseLease(f *testing.F) {
	f.Add([]byte(`{"resource":"a/b","owner":"o","pid":1,"acquired":"2026-01-01T00:00:00Z","expires":"2026-01-01T01:00:00Z"}`))
	f.Add([]byte(`{"resource":1}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		l, ok := parseLease(b)
		if ok && (l.Resource == "" || l.Owner == "") {
			t.Fatal("an incomplete lease was accepted")
		}
	})
}

func FuzzParseTells(f *testing.F) {
	f.Add([]byte(`{"resource":"r","from":"f","text":"t","at":"2026-01-01T00:00:00Z"}` + "\n"))
	f.Add([]byte("x\n\n{\n"))
	f.Fuzz(func(t *testing.T, b []byte) {
		msgs, bad := parseTells(strings.NewReader(string(b)), 256)
		if len(msgs)+bad > len(b)+1 {
			t.Fatalf("%d messages and %d bad lines from %d bytes", len(msgs), bad, len(b))
		}
	})
}

func FuzzParseMeta(f *testing.F) {
	f.Add([]byte(`{"agentType":"general-purpose","description":"d","toolUseId":"t","spawnDepth":1}`))
	f.Add([]byte(`{"spawnDepth":"x"}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		var m agentMeta
		parseMeta(b, &m)
	})
}

func FuzzParseRecord(f *testing.F) {
	f.Add([]byte(`[{"added":"2026-11-18T09:00:00Z","line":"x"}]`))
	f.Add([]byte(`[{"added":1}]`))
	f.Fuzz(func(t *testing.T, b []byte) {
		n, _, ok := parseRecord(b)
		if !ok && n != 0 {
			t.Fatal("a refused record counted entries")
		}
	})
}

func FuzzResolve(f *testing.F) {
	f.Add("go-tex/engine")
	f.Add("github.com/go-ansible/.github")
	f.Add("--note")
	f.Add("../../etc/passwd")
	f.Add("a note, with: punctuation/and#hash")
	f.Add("git@github.com:o/r.git")
	root := f.TempDir()
	os.MkdirAll(filepath.Join(root, "gh", "go-tex", "engine"), 0o755)
	os.MkdirAll(filepath.Join(root, "local", "go-tex-engine-x"), 0o755)
	r := &Resolver{LocalhostRoot: filepath.Join(root, "local"), GithubRoot: filepath.Join(root, "gh")}
	f.Fuzz(func(t *testing.T, s string) {
		key, kind, path := r.Resolve(s)
		switch kind {
		case model.KindRepo, model.KindRepoForm, model.KindOrg, model.KindUnresolved:
		default:
			t.Fatalf("unknown kind %q", kind)
		}
		if path != "" && !strings.HasPrefix(path, root) {
			t.Fatalf("a path outside the clone roots: %q", path)
		}
		if kind != model.KindUnresolved {
			if key == "" {
				t.Fatal("a resolved empty key")
			}
			for _, seg := range strings.Split(key, "/") {
				if seg == ".." || seg == "." || seg == "" {
					t.Fatalf("a resolved key with a %q segment: %q", seg, key)
				}
			}
		}
		if utf8.ValidString(s) && !utf8.ValidString(key) {
			t.Fatalf("invalid UTF-8 from valid input: %q", key)
		}
		r.RepoOfCwd(s)
	})
}
