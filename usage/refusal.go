package usage

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/go-agentsync/agentscope/guard"
)

// refHour is the resolution of the kept refusal counts.
const refHour = 3600

// refKey is the refusals of one gate in one hour.
type refKey struct {
	Hour int64
	Gate string
}

// cachedRef is a count as kept on disk.
type cachedRef struct {
	H int64  `json:"h"`
	G string `json:"g"`
	N int64  `json:"n"`
}

var (
	resultMark = []byte(`"tool_result"`)
	hookMark   = []byte(`hook error`)
	denyMark   = []byte(`permissionDecision`)
)

// resultLine is a transcript entry that can carry a gate's refusal: a tool result.
// Only the shape is decoded; the text is read to tell the gate, never kept.
type resultLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type resultItem struct {
	Type    string          `json:"type"`
	IsError bool            `json:"is_error"`
	Content json.RawMessage `json:"content"`
}

// resultText is a tool result's text: a string, or the text parts of a list.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// foldRefusals counts the gate refusals of one transcript line, and reports
// whether the line was one (so the caller does not read it as an assistant entry).
// A line that mentions a hook without being a refusal -- a file, a test's output --
// is not counted: the refusal must be what the tool result STARTS with.
func (s *Scanner) foldRefusals(line []byte) bool {
	if !bytes.Contains(line, resultMark) || !(bytes.Contains(line, hookMark) || bytes.Contains(line, denyMark)) {
		return false
	}
	var l resultLine
	if json.Unmarshal(line, &l) != nil || l.Type != "user" {
		return false
	}
	at, err := time.Parse(time.RFC3339Nano, l.Timestamp)
	if err != nil {
		return false
	}
	var items []resultItem
	if json.Unmarshal(l.Message.Content, &items) != nil {
		return false
	}
	found := false
	for _, it := range items {
		if it.Type != "tool_result" {
			continue
		}
		body, ok := guard.Refusal(resultText(it.Content), it.IsError)
		if !ok {
			continue
		}
		found = true
		if at.Before(s.refCut) {
			continue
		}
		hour := at.Unix() - at.Unix()%refHour
		s.refs[refKey{hour, guard.Attribute(body)}]++
		s.dirty = true
	}
	return found
}

// Scanned is whether a pass over the transcripts has ended: before it, the
// refusals are not measured, which is not the same as none.
func (s *Scanner) Scanned() bool { return s.scanned }

// Refusals is the gate refusals kept, per hour and gate, oldest first.
func (s *Scanner) Refusals() []guard.Count {
	out := make([]guard.Count, 0, len(s.refs))
	for k, n := range s.refs {
		out = append(out, guard.Count{Hour: k.Hour, Gate: k.Gate, N: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hour != out[j].Hour {
			return out[i].Hour < out[j].Hour
		}
		return out[i].Gate < out[j].Gate
	})
	return out
}

func (s *Scanner) cachedRefs() []cachedRef {
	out := make([]cachedRef, 0, len(s.refs))
	for k, n := range s.refs {
		out = append(out, cachedRef{k.Hour, k.Gate, n})
	}
	return out
}
