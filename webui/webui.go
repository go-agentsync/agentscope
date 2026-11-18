// Package webui builds the browser half of agentscope on first run.
//
// The wasm UI is not committed (it is 25 MB, 5.5 MB gzipped, and would be in
// every clone for ever). `agentscope serve` finds the UI SOURCES that belong to
// its own binary, compiles them with the local Go toolchain for GOOS=js
// GOARCH=wasm into the user's cache directory, and serves that.
//
//   - Where the sources are. After `go install github.com/go-agentsync/agentscope/cmd/agentscope@vX`
//     they are in the module cache at the version the binary was built from
//     (runtime/debug.ReadBuildInfo gives the version, `go env GOMODCACHE` the
//     place). Inside a checkout (a development binary has no version) they are
//     the working tree, found upward from the working directory or from the
//     binary.
//   - Where the build goes. <user cache>/agentscope/<key>, where the key is the
//     version (or, for a checkout, a digest of the sources) and the toolchain's
//     `go version`: a new Go release or a changed source is a new entry, never
//     an overwrite.
//   - How it is written. Into a temporary directory beside the entry, then
//     renamed into place, so a reader never sees half a build and two first runs
//     racing each other cannot corrupt it: whichever rename wins is the entry,
//     the other build is thrown away and its caller uses the winner's.
//   - What it will not build from. A source directory (and every parent up to the
//     module cache) that is not owned by the current user or root, or that is
//     writable by its group or by everyone: a stranger who can write there
//     decides what the compiler runs. Commands are exec'd with an argument list,
//     never through a shell.
//
// `agentscope serve --web DIR` skips all of it.
package webui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"time"
)

// ModulePath is the module the UI sources belong to.
const ModulePath = "github.com/go-agentsync/agentscope"

// Files of a complete bundle.
var files = []string{"index.html", "boot.js", "fsguard.js", "style.css", "wasm_exec.js", "app.wasm"}

// webFiles are copied from web/ as they are.
var webFiles = []string{"index.html", "boot.js", "fsguard.js", "style.css"}

// ErrNoGo is returned when no go binary can be found.
var ErrNoGo = errors.New("agentscope builds its UI on first run and needs the Go toolchain on PATH, which was not found: install Go, or serve a UI you built elsewhere with `agentscope serve --web DIR`")

// Options configures Ensure. The zero value is the production behaviour.
type Options struct {
	// Version is the module version of the running binary; "" reads it from
	// the binary's build info. "(devel)" or "" means a checkout.
	Version string
	// CacheDir is where builds go; "" is <user cache dir>/agentscope.
	CacheDir string
	// Go is the go binary; "" looks it up on PATH.
	Go string
	// Cwd and ExecDir are where a checkout is looked for; "" reads them.
	Cwd, ExecDir string
	// Out receives progress lines; nil discards them.
	Out io.Writer
}

func (o Options) out() io.Writer {
	if o.Out == nil {
		return io.Discard
	}
	return o.Out
}

// goEnv is what the toolchain says about itself.
type goEnv struct {
	GOROOT, GOMODCACHE, GOVERSION string
}

func readGoEnv(ctx context.Context, goBin string) (goEnv, error) {
	out, err := exec.CommandContext(ctx, goBin, "env", "-json", "GOROOT", "GOMODCACHE", "GOVERSION").Output()
	if err != nil {
		return goEnv{}, fmt.Errorf("go env: %w", err)
	}
	var e goEnv
	if err := json.Unmarshal(out, &e); err != nil {
		return goEnv{}, fmt.Errorf("go env: %w", err)
	}
	if e.GOROOT == "" || e.GOVERSION == "" {
		return goEnv{}, errors.New("go env printed no GOROOT or GOVERSION")
	}
	return e, nil
}

// binaryVersion is the module version this binary was built from, or "".
func binaryVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Path == ModulePath && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return ""
}

// escapeModule is the module cache's case encoding: an upper-case letter is "!"
// and its lower-case form.
func escapeModule(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('!')
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// isUIRoot reports whether dir holds the UI sources of this module.
func isUIRoot(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	named := false
	for _, l := range strings.Split(string(b), "\n") {
		if rest, ok := strings.CutPrefix(l, "module "); ok {
			named = strings.TrimSpace(rest) == ModulePath
			break
		}
	}
	if !named {
		return false
	}
	for _, p := range []string{"cmd/agentscope-web", "web/index.html"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
			return false
		}
	}
	return true
}

// findCheckout looks upward from each start for a directory holding the sources.
func findCheckout(starts ...string) string {
	for _, s := range starts {
		if s == "" {
			continue
		}
		for d, _ := filepath.Abs(s); ; d = filepath.Dir(d) {
			if isUIRoot(d) {
				return d
			}
			if d == filepath.Dir(d) {
				break
			}
		}
	}
	return ""
}

// resolveRoot says where the sources are and what keys their build.
func resolveRoot(o Options, modcache string) (root, key string, err error) {
	version := o.Version
	if version == "" {
		version = binaryVersion()
	}
	// A binary built from a modified checkout is stamped "vX.Y.Z+dirty": that
	// is not a version anyone can download, its sources are the checkout.
	var missing error
	if version != "" && version != "(devel)" && !strings.Contains(version, "+dirty") {
		root = filepath.Join(modcache, escapeModule(ModulePath)+"@"+escapeModule(version))
		if isUIRoot(root) {
			if err := checkTrusted(root, modcache); err != nil {
				return "", "", err
			}
			return root, version, nil
		}
		// Not in the module cache: a binary built in a clean checkout of a tag is
		// stamped with that tag too, so a checkout is still a place to look.
		missing = fmt.Errorf("the UI sources for %s %s are not in the module cache (%s): run `go mod download %s@%s`, or serve a UI with --web DIR", ModulePath, version, root, ModulePath, version)
	}
	cwd, exe := o.Cwd, o.ExecDir
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if exe == "" {
		if p, err := os.Executable(); err == nil {
			exe = filepath.Dir(p)
		}
	}
	if root = findCheckout(cwd, exe); root == "" {
		if missing != nil {
			return "", "", missing
		}
		return "", "", errors.New("this binary has no module version and is not inside an agentscope checkout, so it cannot find the UI sources: `go install " + ModulePath + "/cmd/agentscope@latest`, or serve a UI with --web DIR")
	}
	// Nothing under root is read before it is trusted.
	if err := checkTrusted(root, modcache); err != nil {
		return "", "", err
	}
	d, err := digest(root)
	if err != nil {
		return "", "", err
	}
	return root, "dev-" + d, nil
}

// digest hashes the files the wasm is built from.
func digest(root string) (string, error) {
	h := sha256.New()
	for _, p := range []string{"go.mod", "go.sum", "cmd/agentscope-web", "ui", "wire", "model", "web"} {
		err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(p)), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil // an optional path (go.sum of a module with no deps)
				}
				return err
			}
			if d.IsDir() || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), len(b))
			h.Write(b)
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._+-]`)

// entryName is the cache entry for a source key and a toolchain.
func entryName(key, goVersion string) string {
	return unsafeChars.ReplaceAllString(key, "_") + "-" + unsafeChars.ReplaceAllString(goVersion, "_")
}

// complete reports whether dir holds a finished bundle.
func complete(dir string) bool {
	for _, f := range files {
		if st, err := os.Stat(filepath.Join(dir, f)); err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
			return false
		}
	}
	return true
}

// Ensure returns a directory holding the built UI, building it first if this
// version and toolchain have none yet.
func Ensure(ctx context.Context, o Options) (string, error) {
	goBin := o.Go
	if goBin == "" {
		p, err := exec.LookPath("go")
		if err != nil {
			return "", ErrNoGo
		}
		goBin = p
	}
	env, err := readGoEnv(ctx, goBin)
	if err != nil {
		return "", err
	}
	root, key, err := resolveRoot(o, env.GOMODCACHE)
	if err != nil {
		return "", err
	}
	cache := o.CacheDir
	if cache == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("no user cache directory (%v): pass --cache-dir, or serve a UI with --web DIR", err)
		}
		cache = filepath.Join(base, "agentscope")
	}
	final := filepath.Join(cache, entryName(key, env.GOVERSION))
	if complete(final) {
		return final, nil
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return "", err
	}
	start := time.Now()
	fmt.Fprintf(o.out(), "building the UI for %s with %s (first run, one time per version; needs the Go modules, which may download)...\n", key, env.GOVERSION)
	tmp, err := os.MkdirTemp(cache, ".build-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp) // a finished build was renamed away; anything else is debris
	if err := build(ctx, goBin, env, root, tmp); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, final); err != nil {
		// Another first run got there first: its rename won, and its build is
		// as good as ours. Anything else is a real failure.
		if complete(final) {
			fmt.Fprintln(o.out(), "another agentscope finished the same build first; using it")
			return final, nil
		}
		return "", err
	}
	fmt.Fprintf(o.out(), "built the UI in %s\n", time.Since(start).Round(100*time.Millisecond))
	return final, nil
}

// build compiles the wasm and gathers the bundle into dir.
func build(ctx context.Context, goBin string, env goEnv, root, dir string) error {
	cmd := exec.CommandContext(ctx, goBin, "build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w",
		"-o", filepath.Join(dir, "app.wasm"), "./cmd/agentscope-web")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0", "GOWORK=off", "GOFLAGS=-mod=readonly")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build of the UI failed: %w\n%s", err, out)
	}
	for _, f := range webFiles {
		b, err := os.ReadFile(filepath.Join(root, "web", f))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, f), b, 0o644); err != nil {
			return err
		}
	}
	// wasm_exec.js must come from the SAME toolchain that built the wasm.
	var js []byte
	for _, rel := range []string{"lib/wasm/wasm_exec.js", "misc/wasm/wasm_exec.js"} {
		if b, err := os.ReadFile(filepath.Join(env.GOROOT, filepath.FromSlash(rel))); err == nil {
			js = b
			break
		}
	}
	if js == nil {
		return fmt.Errorf("wasm_exec.js not found under %s", env.GOROOT)
	}
	return os.WriteFile(filepath.Join(dir, "wasm_exec.js"), js, 0o644)
}
