// Command covercheck is the CI coverage gate: it reads a Go coverage profile
// and fails when a package, or the whole module, is below its minimum.
//
//	covercheck -profile coverage.out -min total=90 -min ui=100
//
// A package is named by its directory below the module root ("ui", "collect",
// "cmd/agentscope"). It is a Go program, with a test, and not shell and awk,
// because a pipeline cannot tell "no answer" from "wrong query": a package
// with no statements in the profile is an ERROR here, never a pass.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

type minimums map[string]float64

func (m minimums) String() string { return fmt.Sprint(map[string]float64(m)) }

func (m minimums) Set(s string) error {
	k, v, ok := strings.Cut(s, "=")
	if !ok || k == "" {
		return fmt.Errorf("want name=percent, got %q", s)
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 || f > 100 {
		return fmt.Errorf("bad percentage in %q", s)
	}
	m[k] = f
	return nil
}

type tally struct{ stmts, covered int }

func (t tally) percent() float64 {
	if t.stmts == 0 {
		return 0
	}
	return 100 * float64(t.covered) / float64(t.stmts)
}

// parse reads a coverage profile into per-package tallies and a total. module
// is the module path, stripped from each file to name its package.
func parse(r io.Reader, module string) (map[string]tally, tally, error) {
	pk := map[string]tally{}
	var total tally
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	first := true
	blocks := map[string]block{}
	dirs := map[string]string{}
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			if !strings.HasPrefix(line, "mode:") {
				return nil, total, fmt.Errorf("not a coverage profile: first line %q", line)
			}
			continue
		}
		if line == "" {
			continue
		}
		// file:startLine.startCol,endLine.endCol numStmts count
		colon := strings.LastIndex(line, ":")
		fields := strings.Fields(line[colon+1:])
		if colon < 0 || len(fields) != 3 {
			return nil, total, fmt.Errorf("bad profile line %q", line)
		}
		n, err1 := strconv.Atoi(fields[1])
		c, err2 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil || n < 0 {
			return nil, total, fmt.Errorf("bad profile line %q", line)
		}
		// `go test -coverpkg=./...` over several packages writes the same
		// block once per test binary; a block is covered when any binary
		// covered it, and counted once.
		key := line[:colon] + ":" + fields[0]
		b := blocks[key]
		b.stmts, b.covered = n, b.covered || c > 0
		blocks[key] = b
		dirs[key] = path.Dir(strings.TrimPrefix(line[:colon], module+"/"))
	}
	if err := sc.Err(); err != nil {
		return nil, total, err
	}
	if first {
		return nil, total, fmt.Errorf("empty profile")
	}
	for key, b := range blocks {
		t := pk[dirs[key]]
		t.stmts += b.stmts
		total.stmts += b.stmts
		if b.covered {
			t.covered += b.stmts
			total.covered += b.stmts
		}
		pk[dirs[key]] = t
	}
	return pk, total, nil
}

type block struct {
	stmts   int
	covered bool
}

// check returns one message per violated minimum, and one per minimum that
// could not be evaluated.
func check(pk map[string]tally, total tally, min minimums) []string {
	var bad []string
	names := make([]string, 0, len(min))
	for k := range min {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		t := total
		if k != "total" {
			var ok bool
			if t, ok = pk[k]; !ok || t.stmts == 0 {
				bad = append(bad, fmt.Sprintf("%s: no statements in the profile (a gate that measured nothing)", k))
				continue
			}
		}
		if p := t.percent(); p < min[k] {
			bad = append(bad, fmt.Sprintf("%s: %.1f%% of %d statements, minimum %.1f%%", k, p, t.stmts, min[k]))
		}
	}
	return bad
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("covercheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	profile := fs.String("profile", "coverage.out", "the coverage profile")
	module := fs.String("module", "github.com/go-agentsync/agentscope", "the module path")
	min := minimums{}
	fs.Var(min, "min", "name=percent; name is total or a package directory (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(min) == 0 {
		fmt.Fprintln(stderr, "covercheck: no -min given")
		return 2
	}
	f, err := os.Open(*profile)
	if err != nil {
		fmt.Fprintln(stderr, "covercheck:", err)
		return 2
	}
	defer f.Close()
	pk, total, err := parse(f, *module)
	if err != nil {
		fmt.Fprintln(stderr, "covercheck:", err)
		return 2
	}
	names := make([]string, 0, len(pk))
	for k := range pk {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Fprintf(stdout, "%-24s %6.1f%% of %d\n", k, pk[k].percent(), pk[k].stmts)
	}
	fmt.Fprintf(stdout, "%-24s %6.1f%% of %d\n", "total", total.percent(), total.stmts)
	if bad := check(pk, total, min); len(bad) > 0 {
		for _, b := range bad {
			fmt.Fprintln(stderr, "FAIL", b)
		}
		return 1
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
