package main

import "testing"

// noPanic runs f and turns a panic into a test failure that names what was
// running: a guard deleted by a mutant must fail an assertion, not crash the
// test binary (which would also hide every test after it).
func noPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v", what, r)
		}
	}()
	f()
}
