package main

import (
	"errors"
	"runtime"
	"testing"
)

// Each user directory is asked for on its own: a missing cache directory is an
// error even when the config directory is known, and the other way round.
func TestUsageDirsFailOnEitherDirectoryAlone(t *testing.T) {
	boom := errors.New("boom")
	oldCache, oldConfig := userCacheDir, userConfigDir
	defer func() { userCacheDir, userConfigDir = oldCache, oldConfig }()
	ok := func() (string, error) { return t.TempDir(), nil }
	bad := func() (string, error) { return "", boom }

	userCacheDir, userConfigDir = bad, ok
	if c, h, err := usageDirs("", ""); !errors.Is(err, boom) || c != "" || h != "" {
		t.Errorf("cache only: %q %q %v", c, h, err)
	}
	userCacheDir, userConfigDir = ok, bad
	if c, h, err := usageDirs("", ""); !errors.Is(err, boom) || c != "" || h != "" {
		t.Errorf("config only: %q %q %v", c, h, err)
	}
}

// Without a home directory the defaults cannot be built, and that is the error
// the options report, not flags laid over an empty set.
func TestOptionsReportAMissingHomeDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "plan9" {
		t.Skip("the home directory comes from another variable")
	}
	t.Setenv("HOME", "")
	c := &common{active: 1, horizon: 1}
	if _, err := c.options(); err == nil {
		t.Fatal("no home directory is no error")
	}
}
