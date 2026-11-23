package accounts

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A rotation that cannot be done refuses the stamp rather than writing past the
// bound. The blocker is a non-empty directory where accounts.jsonl.1 must go: a
// file cannot be renamed over it.
func TestStampsRefuseWhenTheRotationFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rename semantics")
	}
	dir := t.TempDir()
	s := NewStamps(dir)
	write(t, s.path(), strings.Repeat("x", RotateBytes))
	if err := os.MkdirAll(filepath.Join(s.path()+".1", "keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Append(stamp(1, "aaaa1111")); ok || err == nil {
		t.Fatalf("wrote %v, err %v: a failed rotation must be an error", ok, err)
	}
}

type failingTemp struct {
	name                     string
	chmodErr, writeErr, clos error
	closed                   int
}

func (f *failingTemp) Name() string                    { return f.name }
func (f *failingTemp) Chmod(os.FileMode) error         { return f.chmodErr }
func (f *failingTemp) WriteString(string) (int, error) { return 0, f.writeErr }
func (f *failingTemp) Close() error                    { f.closed++; return f.clos }

// Each step of the atomic write that can fail leaves the old file in place and
// reports the error; the steps that open a file close it.
func TestWriteAliasesReportsEachFailingStep(t *testing.T) {
	boom := errors.New("boom")
	old := createTemp
	defer func() { createTemp = old }()
	for name, ft := range map[string]*failingTemp{
		"chmod": {chmodErr: boom},
		"write": {writeErr: boom},
		"close": {clos: boom},
	} {
		dir := t.TempDir()
		ft.name = filepath.Join(dir, "tmp")
		createTemp = func(string, string) (tempFile, error) { return ft, nil }
		path := filepath.Join(dir, "accounts.toml")
		if err := writeAliases(path, map[string]string{"7567": "Labo"}); !errors.Is(err, boom) {
			t.Errorf("%s: err = %v", name, err)
		}
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s: the target was written", name)
		}
		if ft.closed == 0 {
			t.Errorf("%s: the temporary file was never closed", name)
		}
	}
	createTemp = func(string, string) (tempFile, error) { return nil, boom }
	if err := writeAliases(filepath.Join(t.TempDir(), "a.toml"), nil); !errors.Is(err, boom) {
		t.Errorf("create: %v", err)
	}
}

// pathErrOp is the operation of the *os.PathError an error wraps ("" if none).
func pathErrOp(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Op
	}
	return ""
}

// A directory that cannot be made is reported as that, before anything else is
// tried: the failure must name the mkdir, and the temporary file is never asked
// for.
func TestWriteAliasesStopsWhenTheDirectoryCannotBeMade(t *testing.T) {
	file := filepath.Join(t.TempDir(), "plain")
	write(t, file, "x")
	old := createTemp
	defer func() { createTemp = old }()
	called := 0
	createTemp = func(string, string) (tempFile, error) { called++; return nil, errors.New("reached") }
	err := writeAliases(filepath.Join(file, "sub", "accounts.toml"), nil)
	if err == nil || pathErrOp(err) != "mkdir" || called != 0 {
		t.Fatalf("err %v (op %q), createTemp called %d times", err, pathErrOp(err), called)
	}
}

func TestStampsAppendStopsWhenTheDirectoryCannotBeMade(t *testing.T) {
	file := filepath.Join(t.TempDir(), "plain")
	write(t, file, "x")
	ok, err := NewStamps(filepath.Join(file, "sub")).Append(stamp(1, "aaaa1111"))
	if ok || pathErrOp(err) != "mkdir" {
		t.Fatalf("wrote %v, err %v (op %q)", ok, err, pathErrOp(err))
	}
}

// A line longer than the scanner's buffer is a read error, and a half-read alias
// file must name nothing.
func TestParseAliasesRefusesALineTooLongToScan(t *testing.T) {
	in := "a = \"x\"\n" + "b = \"" + strings.Repeat("y", 5000) + "\"\n"
	m, err := parseAliases(strings.NewReader(in))
	if err == nil || len(m) != 0 {
		t.Fatalf("m %v, err %v: want an error and no alias", m, err)
	}
}

// A bare key is letters, digits, _ and - only; the neighbours of each range are
// refused.
func TestParseAliasesBareKeyAlphabet(t *testing.T) {
	for _, k := range []string{"A-Z_a-z_09", "k"} {
		if m, err := parseAliases(strings.NewReader(k + " = \"v\"\n")); err != nil || m[k] != "v" {
			t.Errorf("%q: %v %v", k, m, err)
		}
	}
	for _, bad := range []string{"/", ":", "@", "[", "`", "{", ".", "!", "é"} {
		if _, err := parseAliases(strings.NewReader("a" + bad + "b = \"v\"\n")); err == nil {
			t.Errorf("bare key with %q accepted", bad)
		}
	}
}

// Latest is the newest stamp of THAT account, not the newest stamp.
func TestTimelineLatestIsPerAccount(t *testing.T) {
	tl := Timeline{[]Stamp{stamp(1, "aaaa1111"), stamp(2, "bbbb2222"), stamp(3, "aaaa1111"), stamp(4, "cccc3333")}}
	if st, ok := tl.Latest("aaaa1111"); !ok || !st.At.Equal(stamp(3, "x").At) {
		t.Errorf("a: %v %v", st, ok)
	}
	if st, ok := tl.Latest("bbbb2222"); !ok || st.AccountHash != "bbbb2222" {
		t.Errorf("b: %v %v", st, ok)
	}
	if st, ok := tl.Latest("dddd4444"); ok {
		t.Errorf("unknown account answered %v", st)
	}
}
