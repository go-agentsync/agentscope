package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mod = "github.com/go-agentsync/agentscope"

const profile = `mode: set
` + mod + `/ui/a.go:1.1,2.2 4 1
` + mod + `/ui/a.go:3.1,4.2 6 0
` + mod + `/cmd/x/main.go:1.1,2.2 10 3
` + mod + `/ui/a.go:1.1,2.2 4 0
` + mod + `/ui/a.go:3.1,4.2 6 0
`

func TestParseAndCheck(t *testing.T) {
	pk, total, err := parse(strings.NewReader(profile), mod)
	if err != nil {
		t.Fatal(err)
	}
	if pk["ui"].stmts != 10 || pk["ui"].covered != 4 || pk["cmd/x"].covered != 10 || total.stmts != 20 || total.covered != 14 {
		t.Errorf("%v %v", pk, total)
	}
	if (tally{}).percent() != 0 {
		t.Error("empty tally")
	}
	if bad := check(pk, total, minimums{"total": 70, "cmd/x": 100}); len(bad) != 0 {
		t.Errorf("%v", bad)
	}
	bad := check(pk, total, minimums{"ui": 100, "total": 80, "missing": 10})
	if len(bad) != 3 || !strings.Contains(strings.Join(bad, "\n"), "a gate that measured nothing") {
		t.Errorf("%v", bad)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "nonsense\n", "mode: set\nno-colon-here\n", "mode: set\nf.go:1.1,2.2 x 1\n", "mode: set\nf.go:1.1,2.2 1\n", "mode: set\nf.go:1.1,2.2 -1 1\n"} {
		if _, _, err := parse(strings.NewReader(in), mod); err == nil {
			t.Errorf("accepted %q", in)
		}
	}
}

func TestMinimumsFlag(t *testing.T) {
	m := minimums{}
	for _, bad := range []string{"", "x", "=5", "x=y", "x=101", "x=-1"} {
		if m.Set(bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if err := m.Set("ui=100"); err != nil || m["ui"] != 100 || m.String() == "" {
		t.Error("ui=100")
	}
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.out")
	os.WriteFile(p, []byte(profile), 0o644)
	var out, errb bytes.Buffer
	if code := run([]string{"-profile", p, "-min", "total=50"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "total") {
		t.Errorf("%d %s %s", code, out.String(), errb.String())
	}
	errb.Reset()
	if code := run([]string{"-profile", p, "-min", "ui=100"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "FAIL ui") {
		t.Errorf("%d %s", code, errb.String())
	}
	for _, args := range [][]string{{"-profile", p}, {"-profile", filepath.Join(dir, "absent"), "-min", "total=1"}, {"-bogus"}, {"-min", "bad"}} {
		if code := run(args, &out, &errb); code != 2 {
			t.Errorf("%v: %d", args, code)
		}
	}
	bad := filepath.Join(dir, "bad.out")
	os.WriteFile(bad, []byte("junk"), 0o644)
	if code := run([]string{"-profile", bad, "-min", "total=1"}, &out, &errb); code != 2 {
		t.Errorf("%d", code)
	}
}
