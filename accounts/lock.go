package accounts

import (
	"errors"
	"os"
	"time"
)

// Lock timing: a stamp is a few hundred bytes, so a holder keeps the lock for
// microseconds; one older than staleAfter belongs to a process that died.
const pollEvery = 2 * time.Millisecond

// Variables so a test can make the wait short.
var (
	staleAfter = 2 * time.Second
	waitFor    = 1500 * time.Millisecond
)

// lock takes an exclusive lock by creating path with O_EXCL, which every
// platform makes atomic, and returns the function that releases it. A lock file
// older than staleAfter is taken over. It gives up after waitFor, so a hook is
// never stuck on it: the stamp is then skipped and the next prompt writes it.
func lock(path string) (unlock func(), err error) {
	deadline := time.Now().Add(waitFor)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			f.Close()
			return func() { os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if fi, serr := os.Stat(path); serr == nil && time.Since(fi.ModTime()) > staleAfter {
			os.Remove(path) // its owner is gone; whoever wins the next create holds it
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("accounts: the stamp lock is busy")
		}
		time.Sleep(pollEvery)
	}
}
