package collect

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
)

func TestResolve(t *testing.T) {
	f := newFixture(t)
	r := &Resolver{LocalhostRoot: f.local, GithubRoot: f.github}
	for _, tc := range []struct {
		in   string
		repo string
		kind model.RepoKind
	}{
		{"go-tex/engine", "go-tex/engine", model.KindRepo},
		{"GitHub.com/Go-Tex/Engine", "go-tex/engine", model.KindRepo},
		{"git@github.com:go-tex/engine.git", "go-tex/engine", model.KindRepo},
		{"go-tex/.github", "go-tex/.github", model.KindRepo},
		{"go-widgets/toolkit", "go-widgets/toolkit", model.KindRepo}, // only a localhost clone
		{"go-widgets/toolkit-extra", "go-widgets/toolkit-extra", model.KindRepoForm},
		{"go-tex/engine#m2", "go-tex/engine", model.KindRepo},
		{"go-tex/engine:fix", "go-tex/engine", model.KindRepo},
		{"go-tex/engine/wave1", "go-tex/engine", model.KindRepo},
		{"unknown-org/repo", "unknown-org/repo", model.KindRepoForm},
		{"go-tex", "go-tex", model.KindOrg},
		{"engine", "engine", model.KindUnresolved},
		{"--note", "--note", model.KindUnresolved},
		{"adding_real_HTTP_2_support", "adding_real_http_2_support", model.KindUnresolved},
		{"a note with spaces/and slash", "a note with spaces/and slash", model.KindUnresolved},
		{"", "", model.KindUnresolved},
		{"../../etc", "../../etc", model.KindUnresolved},
		{"a/b/c/d", "a/b/c/d", model.KindUnresolved},
	} {
		repo, kind, _ := r.Resolve(tc.in)
		if repo != tc.repo || kind != tc.kind {
			t.Errorf("Resolve(%q) = %q %s, want %q %s", tc.in, repo, kind, tc.repo, tc.kind)
		}
	}
	if _, _, p := r.Resolve("go-tex/engine"); p != filepath.Join(f.github, "go-tex", "engine") {
		t.Errorf("path %q", p)
	}
	if _, _, p := r.Resolve("go-widgets/toolkit"); p != filepath.Join(f.local, "go-widgets-toolkit") {
		t.Errorf("path %q", p)
	}
}

func TestRepoOfCwd(t *testing.T) {
	f := newFixture(t)
	r := &Resolver{LocalhostRoot: f.local, GithubRoot: f.github}
	r.Resolve("go-widgets/toolkit") // a lease key teaches where the org ends
	for cwd, want := range map[string]string{
		"":                                "",
		f.local:                           "",
		filepath.Join(f.local, "scratch"): "",
		filepath.Join(f.local, "go-tex-engine-diag"):             "go-tex/engine",
		filepath.Join(f.local, "go-tex-engine-diag", "sub", "x"): "go-tex/engine",
		filepath.Join(f.local, "go-widgets-toolkit"):             "go-widgets/toolkit",
		filepath.Join(f.github, "go-tex", "engine", "pkg"):       "go-tex/engine",
		filepath.Join(f.github, "go-tex"):                        "",
		"/somewhere/else":                                        "",
		filepath.Join(f.local, "..", "elsewhere"):                "",
	} {
		if got := r.RepoOfCwd(cwd); got != want {
			t.Errorf("RepoOfCwd(%q) = %q, want %q", cwd, got, want)
		}
	}
	empty := &Resolver{}
	if empty.RepoOfCwd("/a/b") != "" {
		t.Error("no roots, no repo")
	}
}

func TestResolverIndexIsRefreshedAfterTheTTL(t *testing.T) {
	f := newFixture(t)
	now := time.Now()
	r := &Resolver{LocalhostRoot: f.local, GithubRoot: f.github, TTL: time.Minute, Now: func() time.Time { return now }}
	if _, k, _ := r.Resolve("new-org/new-repo"); k != model.KindRepoForm {
		t.Fatal(k)
	}
	os.MkdirAll(filepath.Join(f.github, "new-org", "new-repo"), 0o755)
	if _, k, _ := r.Resolve("new-org/new-repo"); k != model.KindRepoForm {
		t.Error("the index must be cached within the TTL")
	}
	now = now.Add(2 * time.Minute)
	if _, k, _ := r.Resolve("new-org/new-repo"); k != model.KindRepo {
		t.Error("the index must be rebuilt after the TTL")
	}
}
