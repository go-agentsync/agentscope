//go:build unix

package webui

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

func skipRoot(t *testing.T) {
	t.Helper()
	if os.Getuid() == 0 {
		t.Skip("root reads and writes what a permission bit forbids")
	}
}

// sources makes a directory that isUIRoot accepts.
func sources(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	for _, p := range []string{"cmd/agentscope-web", "web"} {
		if err := os.MkdirAll(filepath.Join(d, filepath.FromSlash(p)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for p, s := range map[string]string{"go.mod": "module " + ModulePath + "\n", "web/index.html": "x",
		"web/boot.js": "x", "web/fsguard.js": "x", "web/style.css": "x"} {
		if err := os.WriteFile(filepath.Join(d, filepath.FromSlash(p)), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func goEnvStub(t *testing.T, goroot, extra string) string {
	t.Helper()
	return fakeGo(t, `case "$1" in
env) echo '{"GOROOT":"`+goroot+`","GOMODCACHE":"/nonexistent","GOVERSION":"go9.9"}';;
build) while [ "$1" != "-o" ]; do shift; done; echo wasm > "$2"
`+extra+`;;
esac`)
}

func TestBinaryVersionIsTheOneOfTheModuleItWasBuiltFrom(t *testing.T) {
	old := readBuildInfo
	defer func() { readBuildInfo = old }()
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Path: ModulePath, Version: "v1.2.3"}}, true
	}
	if got := binaryVersion(); got != "v1.2.3" {
		t.Fatalf("version = %q", got)
	}
}

func TestFindCheckoutSkipsAnEmptyStart(t *testing.T) {
	if got := findCheckout(""); got != "" {
		t.Fatalf("%q", got)
	}
}

func TestATrustedRootInTheModuleCacheIsRefusedWhenItIsWritable(t *testing.T) {
	mc := t.TempDir()
	mod := filepath.Join(mc, "github.com", "go-agentsync", "agentscope@v1.2.3")
	if err := os.MkdirAll(filepath.Join(mod, "cmd", "agentscope-web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(mod, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(mod, "go.mod"), []byte("module "+ModulePath+"\n"), 0o644)
	os.WriteFile(filepath.Join(mod, "web", "index.html"), []byte("x"), 0o644)
	os.Chmod(mod, 0o777)
	t.Cleanup(func() { os.Chmod(mod, 0o755) })
	if _, _, err := resolveRoot(Options{Version: "v1.2.3"}, mc); err == nil || !strings.Contains(err.Error(), "refusing to build from") {
		t.Fatalf("%v", err)
	}
}

// What the UI is built from is hashed whole: a source that cannot be read is a
// refusal to build, never a hash of the rest.
func TestDigestRefusesASourceItCannotRead(t *testing.T) {
	root := sources(t)
	if err := os.MkdirAll(filepath.Join(root, "ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "absent"), filepath.Join(root, "ui", "dangling.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := digest(root); err == nil {
		t.Fatal("a dangling source was hashed")
	}
	if _, _, err := resolveRoot(Options{Version: "(devel)", Cwd: root}, ""); err == nil {
		t.Fatal("a checkout with a dangling source was resolved")
	}
}

func TestDigestRefusesADirectoryItCannotList(t *testing.T) {
	skipRoot(t)
	root := sources(t)
	wire := filepath.Join(root, "wire")
	if err := os.MkdirAll(wire, 0o755); err != nil {
		t.Fatal(err)
	}
	os.Chmod(wire, 0)
	t.Cleanup(func() { os.Chmod(wire, 0o755) })
	if _, err := digest(root); err == nil {
		t.Fatal("an unlistable directory was skipped")
	}
}

func TestEnsureReportsAMissingUserCacheDirectoryAndUsesOneThatExists(t *testing.T) {
	root := sources(t)
	goroot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(goroot, "lib", "wasm"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(goroot, "lib", "wasm", "wasm_exec.js"), []byte("js"), 0o644)
	stub := goEnvStub(t, goroot, "")
	old := userCacheDir
	defer func() { userCacheDir = old }()
	userCacheDir = func() (string, error) { return "", errors.New("no cache") }
	if _, err := Ensure(context.Background(), Options{Go: stub, Cwd: root}); err == nil || !strings.Contains(err.Error(), "--cache-dir") {
		t.Fatalf("%v", err)
	}
	base := t.TempDir()
	userCacheDir = func() (string, error) { return base, nil }
	dir, err := Ensure(context.Background(), Options{Go: stub, Cwd: root})
	if err != nil || filepath.Dir(dir) != filepath.Join(base, "agentscope") {
		t.Fatalf("%q %v", dir, err)
	}
}

func TestEnsureReportsACacheItCannotWriteInto(t *testing.T) {
	skipRoot(t)
	root := sources(t)
	cache := t.TempDir()
	os.Chmod(cache, 0o555)
	t.Cleanup(func() { os.Chmod(cache, 0o755) })
	stub := goEnvStub(t, t.TempDir(), "")
	if _, err := Ensure(context.Background(), Options{Go: stub, Cwd: root, CacheDir: cache}); err == nil {
		t.Fatal("a read-only cache was written into")
	}
}

// A rename that fails while the entry is not complete is a real failure, not a
// lost race.
func TestARenameThatFailsOnAnIncompleteEntryIsAnError(t *testing.T) {
	root := sources(t)
	cache := t.TempDir()
	d, err := digest(root)
	if err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(cache, "dev-"+d+"-go9.9")
	goroot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(goroot, "lib", "wasm"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(goroot, "lib", "wasm", "wasm_exec.js"), []byte("js"), 0o644)
	stub := goEnvStub(t, goroot, `mkdir -p "`+final+`"; echo debris > "`+final+`/junk"`)
	if _, err := Ensure(context.Background(), Options{Go: stub, Cwd: root, CacheDir: cache}); err == nil {
		t.Fatal("an incomplete entry was used")
	}
}

func TestBuildReportsASourceFileOrABundleFileItCannotUse(t *testing.T) {
	stub := goEnvStub(t, t.TempDir(), "")
	env := goEnv{GOROOT: t.TempDir()}
	ctx := context.Background()
	// A root without the web files.
	if err := build(ctx, stub, env, t.TempDir(), t.TempDir()); err == nil {
		t.Error("a root without web files was built")
	}
	// A bundle directory where a file cannot be written.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "index.html"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := build(ctx, stub, env, repoRoot(t), dir); err == nil {
		t.Error("a bundle file that is a directory was written")
	}
}

func TestCheckTrustedRefusesWhatItCannotReadTheOwnerOfOrIsNotOurs(t *testing.T) {
	root := t.TempDir()
	oldStat, oldUID := statFile, getuid
	defer func() { statFile, getuid = oldStat, oldUID }()
	statFile = func(string) (fs.FileInfo, error) { return noOwner{}, nil }
	if err := checkTrusted(root, ""); err == nil || !strings.Contains(err.Error(), "cannot read the owner") {
		t.Errorf("a file without an owner: %v", err)
	}
	statFile = oldStat
	skipRoot(t) // a root-owned tree is trusted by anyone
	getuid = func() int { return os.Getuid() + 1 }
	if err := checkTrusted(root, ""); err == nil || !strings.Contains(err.Error(), "not you") {
		t.Errorf("a tree owned by a stranger: %v", err)
	}
}

// noOwner is a FileInfo whose Sys is not a *syscall.Stat_t.
type noOwner struct{}

func (noOwner) Name() string       { return "x" }
func (noOwner) Size() int64        { return 0 }
func (noOwner) Mode() fs.FileMode  { return 0o755 | fs.ModeDir }
func (noOwner) ModTime() time.Time { return time.Time{} }
func (noOwner) IsDir() bool        { return true }
func (noOwner) Sys() any           { return nil }
