//go:build unix

package webui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// getuid and statFile are replaced by a test: no file can be made to belong to
// a stranger without being root, nor to carry no owner at all.
var (
	getuid   = os.Getuid
	statFile = os.Stat
)

// checkTrusted refuses a source tree a stranger could have written to: root
// and, when it is inside the module cache, every directory above it up to and
// including the module cache must be owned by the current user or by root, and
// not writable by group or others. (Module cache directories are 0555.) For a
// checkout only its root is checked: the rest is the developer's own tree.
func checkTrusted(root, modcache string) error {
	me := uint32(getuid())
	within := false
	if modcache != "" {
		rel, err := filepath.Rel(modcache, root)
		within = err == nil && rel != "." && !strings.HasPrefix(rel, "..")
	}
	for d := root; ; d = filepath.Dir(d) {
		st, err := statFile(d)
		if err != nil {
			return err
		}
		sys, ok := st.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("cannot read the owner of %s", d)
		}
		if sys.Uid != me && sys.Uid != 0 {
			return fmt.Errorf("refusing to build from %s: it is owned by uid %d, not you (uid %d)", d, sys.Uid, me)
		}
		if st.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("refusing to build from %s: it is writable by group or others (%v), so someone else decides what the compiler runs", d, st.Mode().Perm())
		}
		if !within || d == modcache || d == filepath.Dir(d) {
			return nil
		}
	}
}
