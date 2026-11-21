package collect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-agentsync/agentscope/model"
)

// compactWindowEnv is the environment variable Claude Code reads for the context
// size at which it compacts, when its settings name none.
const compactWindowEnv = "CLAUDE_CODE_AUTO_COMPACT_WINDOW"

// maxWindow bounds a window taken from a file or the environment: no model has a
// context of a trillion tokens, and a bar must stay a ratio of something sane.
const maxWindow = 1 << 40

// CompactWindow is the context size, in tokens, at which Claude Code compacts:
// autoCompactWindow of claudeDir/settings.json, else the
// CLAUDE_CODE_AUTO_COMPACT_WINDOW environment variable, else one million. A value
// that is not a positive integer is not a window and falls through to the next
// source. getenv is os.Getenv, or a test's.
func CompactWindow(claudeDir string, getenv func(string) string) int64 {
	if b, err := readSmall(filepath.Join(claudeDir, "settings.json"), 1<<20); err == nil {
		var s struct {
			Window json.Number `json:"autoCompactWindow"`
		}
		if json.Unmarshal(b, &s) == nil {
			if n, ok := window(string(s.Window)); ok {
				return n
			}
		}
	}
	if n, ok := window(getenv(compactWindowEnv)); ok {
		return n
	}
	return model.DefaultCompactWindow
}

// AgentWindows are the compaction windows the agent definitions of
// claudeDir/agents/<type>.md name in their frontmatter (autoCompactWindow), by
// agent type. A type whose file names none, or a bad one, is left out: it runs
// under the session's window.
func AgentWindows(claudeDir string) map[string]int64 {
	files, _ := filepath.Glob(filepath.Join(claudeDir, "agents", "*.md"))
	var out map[string]int64
	for _, f := range files {
		b, err := readSmall(f, 1<<16)
		if err != nil {
			continue
		}
		if n, ok := frontmatterWindow(string(b)); ok {
			if out == nil {
				out = map[string]int64{}
			}
			out[strings.TrimSuffix(filepath.Base(f), ".md")] = n
		}
	}
	return out
}

// frontmatterWindow is the autoCompactWindow of a document's leading "---"
// block.
func frontmatterWindow(doc string) (int64, bool) {
	lines := strings.Split(doc, "\n")
	if strings.TrimSpace(lines[0]) != "---" { // Split always returns one line
		return 0, false
	}
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "---" {
			break
		}
		if k, v, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == "autoCompactWindow" {
			return window(v)
		}
	}
	return 0, false
}

// window parses a positive whole number of tokens.
func window(s string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 || n > maxWindow {
		return 0, false
	}
	return n, true
}

// osGetenv is what Snapshot reads the environment through.
var osGetenv = os.Getenv
