package webui

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// repoRoot is this checkout, found from the test's own directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, _ := os.Getwd()
	root := findCheckout(wd)
	if root == "" {
		t.Fatal("no checkout above " + wd)
	}
	return root
}

// A real first-run build from a clean cache, exactly as `agentscope serve`
// does it: go build for GOOS=js, the web files, wasm_exec.js from the same
// toolchain. This is the CI test that replaced the committed-bundle check.
func TestFirstRunBuildFromACleanCache(t *testing.T) {
	if testing.Short() {
		t.Skip("a real wasm build")
	}
	cache := t.TempDir()
	var out bytes.Buffer
	o := Options{CacheDir: cache, Cwd: repoRoot(t), Out: &out}
	dir, err := Ensure(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if !complete(dir) {
		t.Fatalf("%s is not a complete bundle", dir)
	}
	wasm, _ := os.ReadFile(filepath.Join(dir, "app.wasm"))
	if !bytes.HasPrefix(wasm, []byte("\x00asm")) || len(wasm) < 1<<20 {
		t.Fatalf("app.wasm is %d bytes and not a wasm module", len(wasm))
	}
	if !strings.Contains(out.String(), "building the UI") || !strings.Contains(out.String(), "built the UI in") {
		t.Errorf("progress: %q", out.String())
	}
	if ents, _ := os.ReadDir(cache); len(ents) != 1 {
		t.Errorf("the cache holds %d entries, want exactly the bundle (no build debris)", len(ents))
	}
	// The second run uses the entry and builds nothing.
	out.Reset()
	again, err := Ensure(context.Background(), o)
	if err != nil || again != dir || out.Len() != 0 {
		t.Fatalf("second run: %q %v, output %q", again, err, out.String())
	}
}

// Several first runs at once, on one clean cache: every one gets a complete
// bundle (a build thrown away because another's rename won is fine) and
// nothing is left half-built.
func TestConcurrentFirstRunsLeaveOneCompleteEntry(t *testing.T) {
	if testing.Short() {
		t.Skip("real wasm builds")
	}
	cache := t.TempDir()
	root := repoRoot(t)
	var wg sync.WaitGroup
	dirs := make([]string, 3)
	errs := make([]error, 3)
	for i := range dirs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dirs[i], errs[i] = Ensure(context.Background(), Options{CacheDir: cache, Cwd: root})
		}()
	}
	wg.Wait()
	for i := range dirs {
		if errs[i] != nil {
			t.Fatalf("run %d: %v", i, errs[i])
		}
		if dirs[i] != dirs[0] || !complete(dirs[i]) {
			t.Fatalf("run %d got %q (complete %v), run 0 got %q", i, dirs[i], complete(dirs[i]), dirs[0])
		}
	}
	if ents, _ := os.ReadDir(cache); len(ents) != 1 {
		t.Errorf("the cache holds %d entries after concurrent builds", len(ents))
	}
}

func TestNoGoBinaryNamesTheWayOut(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := Ensure(context.Background(), Options{CacheDir: t.TempDir()})
	if !errors.Is(err, ErrNoGo) || !strings.Contains(err.Error(), "--web DIR") {
		t.Fatalf("%v", err)
	}
}

// fakeGo writes an executable stub standing in for go.
func fakeGo(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub")
	}
	p := filepath.Join(t.TempDir(), "go")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGoEnvFailures(t *testing.T) {
	for name, script := range map[string]string{
		"fails":      "exit 1",
		"not json":   "echo hello",
		"no GOROOT":  `echo '{"GOROOT":"","GOMODCACHE":"/x","GOVERSION":"go1"}'`,
		"no version": `echo '{"GOROOT":"/x","GOMODCACHE":"/x","GOVERSION":""}'`,
	} {
		if _, err := Ensure(context.Background(), Options{Go: fakeGo(t, script), CacheDir: t.TempDir()}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestBuildFailureIsReportedAndLeavesNoDebris(t *testing.T) {
	root := repoRoot(t)
	goroot := t.TempDir()
	stub := fakeGo(t, `case "$1" in
env) echo '{"GOROOT":"`+goroot+`","GOMODCACHE":"/nonexistent","GOVERSION":"go9.9"}';;
build) echo "compile error text"; exit 1;;
esac`)
	cache := t.TempDir()
	_, err := Ensure(context.Background(), Options{Go: stub, CacheDir: cache, Cwd: root})
	if err == nil || !strings.Contains(err.Error(), "compile error text") {
		t.Fatalf("%v", err)
	}
	if ents, _ := os.ReadDir(cache); len(ents) != 0 {
		t.Errorf("a failed build left %d entries", len(ents))
	}
}

func TestBuildNeedsWasmExecFromTheToolchain(t *testing.T) {
	root := repoRoot(t)
	goroot := t.TempDir() // no lib/wasm/wasm_exec.js
	stub := fakeGo(t, `case "$1" in
env) echo '{"GOROOT":"`+goroot+`","GOMODCACHE":"/nonexistent","GOVERSION":"go9.9"}';;
build) while [ "$1" != "-o" ]; do shift; done; echo wasm > "$2";;
esac`)
	_, err := Ensure(context.Background(), Options{Go: stub, CacheDir: t.TempDir(), Cwd: root})
	if err == nil || !strings.Contains(err.Error(), "wasm_exec.js") {
		t.Fatalf("%v", err)
	}
}

// With a fake toolchain the success path, the wasm_exec.js source and the
// rename race are covered without compiling anything.
func TestBuildWithAFakeToolchain(t *testing.T) {
	root := repoRoot(t)
	goroot := t.TempDir()
	os.MkdirAll(filepath.Join(goroot, "lib", "wasm"), 0o755)
	os.WriteFile(filepath.Join(goroot, "lib", "wasm", "wasm_exec.js"), []byte("exec-js"), 0o644)
	stub := fakeGo(t, `case "$1" in
env) echo '{"GOROOT":"`+goroot+`","GOMODCACHE":"/nonexistent","GOVERSION":"go9.9"}';;
build) while [ "$1" != "-o" ]; do shift; done; echo wasm > "$2";;
esac`)
	cache := t.TempDir()
	dir, err := Ensure(context.Background(), Options{Go: stub, CacheDir: cache, Cwd: root})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "wasm_exec.js")); string(b) != "exec-js" {
		t.Errorf("wasm_exec.js %q: it must come from the toolchain's GOROOT", b)
	}
	if !strings.Contains(filepath.Base(dir), "go9.9") || !strings.HasPrefix(filepath.Base(dir), "dev-") {
		t.Errorf("the entry %q is keyed by neither the sources nor the toolchain", filepath.Base(dir))
	}
	// A different toolchain is a different entry.
	stub2 := fakeGo(t, strings.ReplaceAll(`case "$1" in
env) echo '{"GOROOT":"`+goroot+`","GOMODCACHE":"/nonexistent","GOVERSION":"go9.10"}';;
build) while [ "$1" != "-o" ]; do shift; done; echo wasm > "$2";;
esac`, "\r", ""))
	dir2, err := Ensure(context.Background(), Options{Go: stub2, CacheDir: cache, Cwd: root})
	if err != nil || dir2 == dir {
		t.Fatalf("a new toolchain reused %q (%v)", dir, err)
	}
	// A cache directory that cannot be created.
	f := filepath.Join(t.TempDir(), "file")
	os.WriteFile(f, nil, 0o644)
	if _, err := Ensure(context.Background(), Options{Go: stub, CacheDir: filepath.Join(f, "sub"), Cwd: root}); err == nil {
		t.Error("an impossible cache directory was accepted")
	}
}

func TestRenameLostToAnotherBuildUsesTheWinner(t *testing.T) {
	root := repoRoot(t)
	goroot := t.TempDir()
	os.MkdirAll(filepath.Join(goroot, "lib", "wasm"), 0o755)
	os.WriteFile(filepath.Join(goroot, "lib", "wasm", "wasm_exec.js"), []byte("js"), 0o644)
	cache := t.TempDir()
	// The stub plays a rival: while "building" it installs a complete entry
	// at the final name, so this run's rename fails and the winner is used.
	d, _ := digest(root)
	final := filepath.Join(cache, "dev-"+d+"-go9.9")
	stub := fakeGo(t, `case "$1" in
env) echo '{"GOROOT":"`+goroot+`","GOMODCACHE":"/nonexistent","GOVERSION":"go9.9"}';;
build) while [ "$1" != "-o" ]; do shift; done; echo wasm > "$2"
  mkdir -p "`+final+`"; for f in index.html boot.js fsguard.js style.css wasm_exec.js app.wasm; do echo rival > "`+final+`/$f"; done;;
esac`)
	var out bytes.Buffer
	dir, err := Ensure(context.Background(), Options{Go: stub, CacheDir: cache, Cwd: root, Out: &out})
	if err != nil || dir != final || !strings.Contains(out.String(), "finished the same build first") {
		t.Fatalf("%q %v %q", dir, err, out.String())
	}
	if b, _ := os.ReadFile(filepath.Join(final, "app.wasm")); string(b) != "rival\n" {
		t.Errorf("the winner's entry was overwritten: %q", b)
	}
	if ents, _ := os.ReadDir(cache); len(ents) != 1 {
		t.Errorf("%d entries: the losing build must be removed", len(ents))
	}
}

func TestResolveRoot(t *testing.T) {
	mc := t.TempDir()
	mod := filepath.Join(mc, "github.com", "go-agentsync", "agentscope@v1.2.3")
	os.MkdirAll(filepath.Join(mod, "cmd", "agentscope-web"), 0o755)
	os.MkdirAll(filepath.Join(mod, "web"), 0o755)
	os.WriteFile(filepath.Join(mod, "go.mod"), []byte("// c\nmodule "+ModulePath+"\n\ngo 1.27\n"), 0o644)
	os.WriteFile(filepath.Join(mod, "web", "index.html"), []byte("x"), 0o644)

	root, key, err := resolveRoot(Options{Version: "v1.2.3"}, mc)
	if err != nil || root != mod || key != "v1.2.3" {
		t.Fatalf("%q %q %v", root, key, err)
	}
	if _, _, err := resolveRoot(Options{Version: "v9.9.9"}, mc); err == nil || !strings.Contains(err.Error(), "--web DIR") {
		t.Errorf("a version missing from the module cache: %v", err)
	}
	// No version and no checkout above the given places.
	if _, _, err := resolveRoot(Options{Version: "(devel)", Cwd: t.TempDir(), ExecDir: t.TempDir()}, mc); err == nil || !strings.Contains(err.Error(), "--web DIR") {
		t.Errorf("no version, no checkout: %v", err)
	}
	// A checkout is found from the binary's directory too.
	if root, key, err := resolveRoot(Options{Version: "(devel)", Cwd: t.TempDir(), ExecDir: filepath.Join(mod, "cmd")}, mc); err != nil || root != mod || !strings.HasPrefix(key, "dev-") {
		t.Errorf("%q %q %v", root, key, err)
	}
	// The key follows the sources.
	_, k1, _ := resolveRoot(Options{Version: "(devel)", Cwd: mod}, mc)
	os.WriteFile(filepath.Join(mod, "web", "index.html"), []byte("changed"), 0o644)
	_, k2, _ := resolveRoot(Options{Version: "(devel)", Cwd: mod}, mc)
	if k1 == k2 {
		t.Error("a changed source kept the cache key")
	}
	// Defaults read the build info and the working directory without failing.
	_, _, _ = resolveRoot(Options{}, mc)
	if binaryVersion() != "" {
		t.Log("test binary has a module version:", binaryVersion())
	}
	if escapeModule("github.com/Foo/Bar") != "github.com/!foo/!bar" {
		t.Error("escapeModule")
	}
	if got := entryName("v1.2.3+x/y", "go1.27.4 X"); strings.ContainsAny(got, "/ ") {
		t.Errorf("entryName %q", got)
	}
	if isUIRoot(t.TempDir()) {
		t.Error("an empty directory is not the UI root")
	}
	bad := t.TempDir()
	os.WriteFile(filepath.Join(bad, "go.mod"), []byte("module example.com/other\n"), 0o644)
	if isUIRoot(bad) {
		t.Error("another module is not the UI root")
	}
	os.WriteFile(filepath.Join(bad, "go.mod"), []byte("module "+ModulePath+"\n"), 0o644)
	if isUIRoot(bad) {
		t.Error("a module without the UI directories is not the UI root")
	}
	if d, err := digest(t.TempDir()); err != nil || len(d) != 16 {
		t.Errorf("digest of a tree without the optional sources: %q %v", d, err)
	}
	if complete(t.TempDir()) {
		t.Error("an empty directory is a complete bundle")
	}
}

func TestEnsureRefusesAnUntrustedSourceTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	checkout := t.TempDir()
	os.MkdirAll(filepath.Join(checkout, "cmd", "agentscope-web"), 0o755)
	os.MkdirAll(filepath.Join(checkout, "web"), 0o755)
	os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module "+ModulePath+"\n"), 0o644)
	os.WriteFile(filepath.Join(checkout, "web", "index.html"), []byte("x"), 0o644)
	goroot := t.TempDir()
	stub := fakeGo(t, `echo '{"GOROOT":"`+goroot+`","GOMODCACHE":"/nonexistent","GOVERSION":"go9.9"}'`)
	// Group- and world-writable: someone else decides what the compiler runs.
	for _, mode := range []os.FileMode{0o775, 0o757, 0o777} {
		os.Chmod(checkout, mode)
		_, err := Ensure(context.Background(), Options{Go: stub, CacheDir: t.TempDir(), Cwd: checkout})
		if err == nil || !strings.Contains(err.Error(), "refusing to build from") {
			t.Errorf("mode %v: %v", mode, err)
		}
	}
	os.Chmod(checkout, 0o755)
	if err := checkTrusted(checkout, ""); err != nil {
		t.Errorf("an owner-only tree was refused: %v", err)
	}
	if err := checkTrusted(filepath.Join(checkout, "absent"), ""); err == nil {
		t.Error("a missing directory is not trusted")
	}
}

func TestCheckTrustedWalksUpToTheModuleCache(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	mc := t.TempDir()
	mid := filepath.Join(mc, "github.com", "x")
	mod := filepath.Join(mid, "agentscope@v1")
	os.MkdirAll(mod, 0o755)
	if err := checkTrusted(mod, mc); err != nil {
		t.Fatal(err)
	}
	os.Chmod(mid, 0o777) // a parent inside the module cache
	t.Cleanup(func() { os.Chmod(mid, 0o755) })
	if err := checkTrusted(mod, mc); err == nil || !strings.Contains(err.Error(), mid) {
		t.Errorf("a writable parent inside the module cache must be refused: %v", err)
	}
	os.Chmod(mid, 0o755)
	os.Chmod(mc, 0o777)
	t.Cleanup(func() { os.Chmod(mc, 0o755) })
	if err := checkTrusted(mod, mc); err == nil {
		t.Error("a writable module cache must be refused")
	}
}
