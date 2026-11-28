package demo

import "testing"

// noPanic runs f and turns a panic into a test failure that names what was
// running. A guard whose deletion lets the code dereference nil is otherwise
// "caught" only because the test binary crashed, which asserts nothing about
// the guard and hides every test after it; here the crash becomes an assertion.
func noPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v", what, r)
		}
	}()
	f()
}
