package collect

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-agentsync/agentsync/lease"
)

// parseLease decodes one lease file; a torn or incomplete one is not a lease.
func parseLease(b []byte) (lease.Lease, bool) {
	var l lease.Lease
	if err := json.Unmarshal(b, &l); err != nil {
		return lease.Lease{}, false
	}
	if l.Resource == "" || l.Owner == "" {
		return lease.Lease{}, false
	}
	return l, true
}

// readLeases reads every lease file, re-reading only those that changed.
func (c *Collector) readLeases(now time.Time, st *model.Stats, addAnom func(k, s, d string)) []lease.Lease {
	dir := filepath.Join(c.opt.ClaudeDir, "agentsync", "locks")
	ents, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			st.ReadErrors++
			addAnom(model.AnomalyUnreadable, dir, err.Error())
		}
		return nil
	}
	var out []lease.Lease
	seen := map[string]bool{}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".lease") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		seen[p] = true
		info, err := e.Info()
		if err != nil {
			continue
		}
		k := fileKey{info.ModTime(), info.Size()}
		cl, ok := c.leases[p]
		if !ok || cl.fileKey != k {
			cl = cachedLease{fileKey: k}
			if b, err := readSmall(p, 64<<10); err == nil {
				cl.l, cl.ok = parseLease(b)
			}
			c.leases[p] = cl
		}
		st.LeasesRead++
		if !cl.ok {
			st.LeasesTorn++
			continue
		}
		out = append(out, cl.l)
	}
	for p := range c.leases {
		if !seen[p] {
			delete(c.leases, p)
		}
	}
	return out
}

// parseTells decodes a .messages file: one JSON object per line. A line that
// does not parse is counted, not skipped in silence.
func parseTells(r io.Reader, maxLine int) (msgs []lease.Message, bad int) {
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, _, complete, err := readLine(br, maxLine)
		if !complete {
			// End of input. A last line with no newline is not counted: Tell
			// writes body and newline in one append, so it is a torn write
			// still in flight.
			_ = err
			break
		}
		if line == nil {
			bad++
			continue
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var m lease.Message
		if err := json.Unmarshal(line, &m); err != nil {
			bad++
			continue
		}
		msgs = append(msgs, m)
	}
	return msgs, bad
}

// readTells reads the .messages files, keeping the messages inside the window.
func (c *Collector) readTells(now time.Time, st *model.Stats) []lease.Message {
	dir := filepath.Join(c.opt.ClaudeDir, "agentsync", "locks")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []lease.Message
	seen := map[string]bool{}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".messages") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		seen[p] = true
		info, err := e.Info()
		if err != nil {
			continue
		}
		k := fileKey{info.ModTime(), info.Size()}
		ct, ok := c.tells[p]
		if !ok || ct.fileKey != k {
			ct = cachedTells{fileKey: k}
			if f, err := os.Open(p); err == nil {
				ct.msgs, ct.dropped = parseTells(io.LimitReader(f, maxSmallFile), c.opt.MaxLine)
				f.Close()
			} else {
				st.ReadErrors++
			}
			c.tells[p] = ct
		}
		st.TellsDropped += ct.dropped
		for _, m := range ct.msgs {
			st.TellsRead++
			if c.opt.TellWindow > 0 && now.Sub(m.At) > c.opt.TellWindow {
				st.TellsDropped++
				continue
			}
			out = append(out, m)
		}
	}
	for p := range c.tells {
		if !seen[p] {
			delete(c.tells, p)
		}
	}
	return out
}

// recInfo is what is kept of an agentsync session record: how many entries it
// has and when the last was added. The entries' text is a memory index line --
// content -- and is decoded into nothing.
type recInfo struct {
	n    int
	last time.Time
	mod  time.Time
}

// parseRecord counts the entries of a session record and finds the latest
// "added" stamp.
func parseRecord(b []byte) (n int, last time.Time, ok bool) {
	var v []struct {
		Added time.Time `json:"added"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return 0, time.Time{}, false
	}
	for _, e := range v {
		if e.Added.After(last) {
			last = e.Added
		}
	}
	return len(v), last, true
}

func (c *Collector) readRecords(st *model.Stats) map[string]recInfo {
	dir := filepath.Join(c.opt.ClaudeDir, "agentsync", "sessions")
	ents, err := os.ReadDir(dir)
	out := map[string]recInfo{}
	if err != nil {
		return out
	}
	seen := map[string]bool{}
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !isUUID(id) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		seen[p] = true
		info, err := e.Info()
		if err != nil {
			continue
		}
		k := fileKey{info.ModTime(), info.Size()}
		cr, ok := c.recs[p]
		if !ok || cr.fileKey != k {
			cr = cachedRec{fileKey: k}
			if b, err := readSmall(p, 32<<20); err == nil {
				cr.n, cr.last, _ = parseRecord(b)
			} else {
				st.ReadErrors++
			}
			c.recs[p] = cr
		}
		st.SessionRecords++
		out[id] = recInfo{n: cr.n, last: cr.last, mod: info.ModTime()}
	}
	for p := range c.recs {
		if !seen[p] {
			delete(c.recs, p)
		}
	}
	return out
}
