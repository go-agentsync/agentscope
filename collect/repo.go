package collect

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-agentsync/agentsync/lease"
)

// Resolver turns what a lease calls its resource into a repository, when it is
// one. A lease key is whatever a session typed: "go-tex/engine",
// "github.com/go-ansible/.github", and also a task note used as the key, or a
// flag ("--note"). The first two are repositories; the rest are anomalies the
// UI must show, not drop.
type Resolver struct {
	LocalhostRoot string // <org>-<repo>[-topic] clones
	GithubRoot    string // <org>/<repo> clones
	TTL           time.Duration
	Now           func() time.Time

	mu      sync.Mutex
	built   time.Time
	github  map[string]string // org/repo -> path
	orgs    map[string]string // org -> path
	local   []string          // lower-cased directory names under LocalhostRoot
	localAt map[string]string // lower-cased name -> real path
	dirKey  map[string]string // "org-repo" -> "org/repo"
	learned map[string]string // the same, from lease keys that matched a localhost clone
	cache   map[string]resolved
}

func (r *Resolver) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// refresh rebuilds the index of clones when it is older than TTL.
func (r *Resolver) refresh() {
	r.mu.Lock()
	defer r.mu.Unlock()
	ttl := r.TTL
	if ttl == 0 {
		ttl = time.Minute
	}
	if r.github != nil && r.now().Sub(r.built) < ttl {
		return
	}
	r.built = r.now()
	r.cache = nil
	r.github = map[string]string{}
	r.orgs = map[string]string{}
	r.local = nil
	r.localAt = map[string]string{}
	r.dirKey = map[string]string{}
	if r.GithubRoot != "" {
		orgs, _ := os.ReadDir(r.GithubRoot)
		for _, o := range orgs {
			if !o.IsDir() || strings.HasPrefix(o.Name(), ".") {
				continue
			}
			org := strings.ToLower(o.Name())
			r.orgs[org] = filepath.Join(r.GithubRoot, o.Name())
			repos, _ := os.ReadDir(filepath.Join(r.GithubRoot, o.Name()))
			for _, rp := range repos {
				if !rp.IsDir() {
					continue
				}
				key := org + "/" + strings.ToLower(rp.Name())
				r.github[key] = filepath.Join(r.GithubRoot, o.Name(), rp.Name())
				r.dirKey[org+"-"+strings.ToLower(rp.Name())] = key
			}
		}
	}
	if r.LocalhostRoot != "" {
		ents, _ := os.ReadDir(r.LocalhostRoot)
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			n := strings.ToLower(e.Name())
			r.local = append(r.local, n)
			r.localAt[n] = filepath.Join(r.LocalhostRoot, e.Name())
		}
	}
}

// validSegment reports whether s can be one segment of an org/repo name.
func validSegment(s string) bool {
	if s == "" || s == "." || s == ".." || len(s) > 100 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '.', c == '-', c == '_':
		default:
			return false
		}
	}
	return s[0] != '-' // a leading dash is a flag, not a name
}

type resolved struct {
	key  string
	kind model.RepoKind
	path string
}

// Resolve classifies a lease resource. The answer is cached until the index of
// clones is rebuilt: a snapshot resolves every lease, hundreds of them, and
// the same keys come back every tick.
func (r *Resolver) Resolve(resource string) (key string, kind model.RepoKind, path string) {
	r.refresh()
	r.mu.Lock()
	c, ok := r.cache[resource]
	r.mu.Unlock()
	if ok {
		return c.key, c.kind, c.path
	}
	key, kind, path = r.resolve(resource)
	r.mu.Lock()
	if r.cache == nil {
		r.cache = map[string]resolved{}
	}
	if len(r.cache) < 1<<16 { // bounded: lease keys are untrusted text
		r.cache[resource] = resolved{key, kind, path}
	}
	r.mu.Unlock()
	return key, kind, path
}

func (r *Resolver) resolve(resource string) (key string, kind model.RepoKind, path string) {
	key = lease.Normalize(resource)
	// "org/repo#topic" and "org/repo:topic" name a repository and a topic.
	base := key
	if i := strings.IndexAny(base, "#:"); i > 0 {
		base = base[:i]
	}
	parts := strings.Split(base, "/")
	if len(parts) > 2 { // "org/repo/topic"
		if k, kind, p := r.Resolve(strings.Join(parts[:2], "/")); kind == model.KindRepo {
			return k, kind, p
		}
	}
	for _, p := range parts {
		if !validSegment(p) {
			return key, model.KindUnresolved, ""
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key = base
	switch len(parts) {
	case 1:
		if p, ok := r.orgs[parts[0]]; ok {
			return key, model.KindOrg, p
		}
	case 2:
		if p, ok := r.github[key]; ok {
			return key, model.KindRepo, p
		}
		stem := parts[0] + "-" + parts[1]
		for _, n := range r.local {
			if n == stem || strings.HasPrefix(n, stem+"-") {
				// A directory name does not say where the org ends and the
				// repo begins; a lease key does. Remember the split.
				if r.learned == nil {
					r.learned = map[string]string{}
				}
				r.learned[stem] = key
				return key, model.KindRepo, r.localAt[n]
			}
		}
		return key, model.KindRepoForm, ""
	}
	return key, model.KindUnresolved, ""
}

// RepoOfCwd names the repository a working directory is inside, or "".
func (r *Resolver) RepoOfCwd(cwd string) string {
	if cwd == "" {
		return ""
	}
	r.refresh()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.LocalhostRoot != "" {
		if rel, ok := under(cwd, r.LocalhostRoot); ok {
			dir := strings.ToLower(strings.SplitN(rel, string(filepath.Separator), 2)[0])
			// <org>-<repo>-<topic>: strip trailing segments until it is a repo.
			for dir != "" {
				if k, ok := r.dirKey[dir]; ok {
					return k
				}
				if k, ok := r.learned[dir]; ok {
					return k
				}
				i := strings.LastIndex(dir, "-")
				if i < 0 {
					break
				}
				dir = dir[:i]
			}
			return ""
		}
	}
	if r.GithubRoot != "" {
		if rel, ok := under(cwd, r.GithubRoot); ok {
			p := strings.Split(rel, string(filepath.Separator))
			if len(p) >= 2 {
				return strings.ToLower(p[0] + "/" + p[1])
			}
		}
	}
	return ""
}

// under returns p relative to root when p is inside root (not equal to it).
func under(p, root string) (string, bool) {
	rel, err := filepath.Rel(root, filepath.Clean(p))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}
