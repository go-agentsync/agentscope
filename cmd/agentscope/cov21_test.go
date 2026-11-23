package main

import (
	"bytes"
	"errors"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
)

// noHome leaves the process without a home directory, which is what makes the
// default directories unknowable.
func noHome(t *testing.T) {
	t.Helper()
	for _, k := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "APPDATA", "LocalAppData"} {
		t.Setenv(k, "")
	}
}

func TestVersionIsTheModuleVersionOfAReleaseBuild(t *testing.T) {
	old := readBuildInfo
	defer func() { readBuildInfo = old }()
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, true
	}
	if got := version(); got != "v1.2.3" {
		t.Fatalf("version = %q", got)
	}
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true
	}
	if got := version(); got != "devel" {
		t.Fatalf("version = %q", got)
	}
}

func TestOptionsReportAHomeThatCannotBeFound(t *testing.T) {
	noHome(t)
	c := &common{}
	if _, err := c.options(); err == nil {
		t.Fatal("no home directory, and no error")
	}
}

func TestStampReportsAConfigDirectoryThatCannotBeFound(t *testing.T) {
	f := claudeFile(t, "u", "")
	noHome(t)
	if why := stamp(strings.NewReader("{}"), f, ""); why == "" {
		t.Fatal("no config directory is reported")
	}
}

func TestServeRefusesWhatItCannotServeFrom(t *testing.T) {
	boom := errors.New("no absolute path")
	oldAbs, oldConfig := absPath, userConfigDir
	defer func() { absPath, userConfigDir = oldAbs, oldConfig }()
	absPath = func(string) (string, error) { return "", boom }
	if err := serveRefusing(t); !errors.Is(err, boom) {
		t.Errorf("an absolute path that cannot be made: %v", err)
	}
	absPath = oldAbs
	if err := serveRefusing(t, "--web", t.TempDir()); err == nil || !strings.Contains(err.Error(), "no UI in") {
		t.Errorf("a directory without index.html: %v", err)
	}
	userConfigDir = func() (string, error) { return "", boom }
	if err := serveRefusing(t, "--history-dir", ""); !errors.Is(err, boom) {
		t.Errorf("a config directory that cannot be found: %v", err)
	}
}

// --show-org only adds the organisation's name to the Rename dialog; the server
// still serves until it is stopped.
func TestServeAcceptsShowOrg(t *testing.T) {
	if err := serveRefusing(t, "--show-org"); err != nil {
		t.Fatalf("%v", err)
	}
}

func TestRenderListsASessionWithoutTranscriptAndTheAnomalies(t *testing.T) {
	var b bytes.Buffer
	render(&b, model.Snapshot{
		At:        time.Now(),
		Sessions:  []model.Session{{ID: "cccccccc-3333-4333-8333-333333333333", Short: "cccccccc"}},
		Anomalies: []model.Anomaly{{Kind: "odd", Subject: "s", Detail: "d"}},
	})
	for _, want := range []string{"cccccccc no-tr ", "ANOMALIES (1)", "odd: s d"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("no %q in:\n%s", want, b.String())
		}
	}
}
