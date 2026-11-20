// Package accounts is what agentscope knows about WHO a call was made as: the
// signed-in account (read from ~/.claude.json), the stamps the `stamp` hook
// appends when it changes, and the display names the owner gives accounts.
//
// An account is known by a short hash of its id and a label; its e-mail and its
// id are never kept. The organisation's name is read only when the owner asks
// for it (--show-org), and only to suggest a display name.
package accounts

import (
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/go-agentsync/agentscope/quota"
)

// maxClaudeJSON bounds ~/.claude.json, which holds the whole project history and
// grows to tens of megabytes.
const maxClaudeJSON = 32 << 20

// Identity is the signed-in account as agentscope names it.
type Identity struct {
	// Key is the 8 hex digit hash of the account id (see quota.AccountKey).
	Key string
	// Label is "Claude Code", and its plan in brackets when one is known.
	Label string
	// Plan is the subscription ("team", "max", ...), "" when unknown.
	Plan string
	// Org is the organisation's name: set only when the caller asked for it.
	Org string
}

// ReadIdentity reads the signed-in account from the .claude.json at path. ok is
// false when the file cannot be read, is no JSON object, or names no account:
// nothing is guessed. withOrg says whether the organisation's name is read.
func ReadIdentity(path string, withOrg bool) (id Identity, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return Identity{}, false
	}
	defer f.Close()
	var c struct {
		OAuth struct {
			ID   string `json:"accountUuid"`
			Type string `json:"organizationType"`
			Org  string `json:"organizationName"`
		} `json:"oauthAccount"`
	}
	if json.NewDecoder(io.LimitReader(f, maxClaudeJSON)).Decode(&c) != nil || c.OAuth.ID == "" {
		return Identity{}, false
	}
	id = Identity{Key: quota.AccountKey(c.OAuth.ID), Plan: planOf(c.OAuth.Type)}
	id.Label = "Claude Code"
	if id.Plan != "" {
		id.Label += " (" + id.Plan + ")"
	}
	if withOrg {
		id.Org = clip(c.OAuth.Org, 80)
	}
	return id, true
}

// planOf is the plan in an organisationType ("claude_team" is "team").
func planOf(t string) string {
	return clip(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(t)), "claude_"), 32)
}

// clip bounds a string taken from a file to n bytes, on a rune boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}
