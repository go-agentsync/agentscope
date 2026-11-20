package accounts

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Stamp says that from At on, calls were made as an account: what the `stamp`
// hook appends, once per change of account, to accounts.jsonl.
type Stamp struct {
	At          time.Time `json:"at"`
	SessionID   string    `json:"session_id,omitempty"`
	AccountHash string    `json:"account_hash"`
	Label       string    `json:"label,omitempty"`
	Plan        string    `json:"plan,omitempty"`
}

// Bounds on the stamp file.
const (
	// RotateBytes is the size past which accounts.jsonl is renamed to
	// accounts.jsonl.1 (the previous one is dropped): a stamp is written once per
	// CHANGE of account, so a file this large is years of switching.
	RotateBytes = 256 << 10
	maxLine     = 4 << 10
	// tailBytes is how much of the end of the file the hook reads to learn the
	// last stamp: several lines, and not the whole history.
	tailBytes = 8 << 10
)

// Stamps is the stamp file in a directory.
type Stamps struct{ dir string }

// NewStamps keeps stamps in dir/accounts.jsonl; dir is created when first
// written.
func NewStamps(dir string) *Stamps { return &Stamps{dir: dir} }

func (s *Stamps) path() string { return filepath.Join(s.dir, "accounts.jsonl") }

// Append adds st, unless the last stamp already names the same account, which
// reports false. It is safe against any number of concurrent writers (sessions
// share the file): the check and the append happen under one lock.
func (s *Stamps) Append(st Stamp) (wrote bool, err error) {
	if st.AccountHash == "" {
		return false, errors.New("accounts: a stamp needs an account hash")
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return false, err
	}
	unlock, err := lock(filepath.Join(s.dir, "accounts.lock"))
	if err != nil {
		return false, err
	}
	defer unlock()

	if last, ok := s.last(); ok && last.AccountHash == st.AccountHash {
		return false, nil
	}
	if fi, err := os.Stat(s.path()); err == nil && fi.Size() >= RotateBytes {
		if err := os.Rename(s.path(), s.path()+".1"); err != nil {
			return false, err
		}
	}
	line, err := json.Marshal(st)
	if err != nil {
		return false, err
	}
	f, err := os.OpenFile(s.path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return false, err
	}
	_, err = f.Write(append(line, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err == nil, err
}

// last is the newest stamp kept: the end of accounts.jsonl, or, just after a
// rotation emptied it, the end of accounts.jsonl.1.
func (s *Stamps) last() (Stamp, bool) {
	for _, p := range []string{s.path(), s.path() + ".1"} {
		if st, ok := lastOf(p); ok {
			return st, true
		}
	}
	return Stamp{}, false
}

// lastOf reads the last well-formed stamp in the tail of a file.
func lastOf(path string) (Stamp, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Stamp{}, false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return Stamp{}, false
	}
	off := max(fi.Size()-tailBytes, 0)
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return Stamp{}, false
	}
	b, err := io.ReadAll(io.LimitReader(f, tailBytes))
	if err != nil {
		return Stamp{}, false
	}
	if off > 0 { // the first line of the window is cut in the middle
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	lines := bytes.Split(b, []byte{'\n'})
	for i := len(lines) - 1; i >= 0; i-- {
		var st Stamp
		if json.Unmarshal(lines[i], &st) == nil && st.AccountHash != "" {
			return st, true
		}
	}
	return Stamp{}, false
}

// Timeline is the stamps, oldest first.
type Timeline struct{ stamps []Stamp }

// Load reads every stamp kept, oldest first. A line that does not parse is
// skipped: a torn write or a hand edit must not hide the rest.
func (s *Stamps) Load() Timeline {
	var all []Stamp
	for _, p := range []string{s.path() + ".1", s.path()} {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		br := bufio.NewReaderSize(io.LimitReader(f, 2*RotateBytes), 16<<10)
		for {
			line, err := br.ReadSlice('\n')
			if errors.Is(err, bufio.ErrBufferFull) { // a line past the buffer is garbage
				for errors.Is(err, bufio.ErrBufferFull) {
					_, err = br.ReadSlice('\n')
				}
				line = nil
			}
			var st Stamp
			if len(line) > 0 && len(line) <= maxLine && json.Unmarshal(bytes.TrimSpace(line), &st) == nil && st.AccountHash != "" && !st.At.IsZero() {
				all = append(all, st)
			}
			if err != nil {
				break
			}
		}
		f.Close()
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].At.Before(all[j].At) })
	return Timeline{all}
}

// At is the account a call at t was made as: the latest stamp at or before t.
// ok is false when no stamp is that old.
func (tl Timeline) At(t time.Time) (Stamp, bool) {
	i := sort.Search(len(tl.stamps), func(i int) bool { return tl.stamps[i].At.After(t) })
	if i == 0 {
		return Stamp{}, false
	}
	return tl.stamps[i-1], true
}

// Latest is the newest stamp of the account with the hash, the one that carries
// its label and plan as the hook last saw them.
func (tl Timeline) Latest(hash string) (Stamp, bool) {
	for i := len(tl.stamps) - 1; i >= 0; i-- {
		if tl.stamps[i].AccountHash == hash {
			return tl.stamps[i], true
		}
	}
	return Stamp{}, false
}

// Len is the number of stamps.
func (tl Timeline) Len() int { return len(tl.stamps) }
