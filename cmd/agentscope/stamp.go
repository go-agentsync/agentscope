package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/go-agentsync/agentscope/accounts"
	"github.com/spf13/cobra"
)

// maxHookPayload bounds what is read from stdin: a hook payload is a few hundred
// bytes, but a UserPromptSubmit one carries the prompt.
const maxHookPayload = 4 << 20

// stampNow is the clock `stamp` uses; tests replace it.
var stampNow = time.Now

func stampCmd() *cobra.Command {
	var claudeJSON, dir string
	var verbose bool
	cmd := &cobra.Command{
		Use:   "stamp",
		Short: "Record which account this session runs as (a SessionStart and UserPromptSubmit hook)",
		Long: `stamp is meant to run as a Claude Code hook, on SessionStart and on
UserPromptSubmit. It reads the hook payload on stdin, reads the signed-in
account from ~/.claude.json, and appends one line

  {"at", "session_id", "account_hash", "label", "plan"}

to <user config dir>/agentscope/accounts.jsonl, but only when the account
differs from the last line written (sessions share the file; the append is
locked and the file is rotated). The Quota tab uses these lines to give an
account the tokens of a session that has no session-owner entry.

It prints NOTHING on stdout (a hook's stdout is injected into the model's
context), takes no network, and always exits 0: a hook that failed must not
block a prompt. --verbose says on stderr why a stamp was not written.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if why := stamp(cmd.InOrStdin(), claudeJSON, dir); why != "" && verbose {
				fmt.Fprintln(cmd.ErrOrStderr(), "agentscope stamp:", why)
			}
			return nil
		},
	}
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&claudeJSON, "claude-json", filepath.Join(home, ".claude.json"), "the Claude Code account file (read-only)")
	cmd.Flags().StringVar(&dir, "dir", "", "where accounts.jsonl is kept (default <user config dir>/agentscope)")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "say on stderr why nothing was written")
	return cmd
}

// stamp does the work and returns why nothing was written, "" when a line was
// or the account had not changed.
func stamp(in io.Reader, claudeJSON, dir string) string {
	var payload struct {
		SessionID string `json:"session_id"`
		Timestamp string `json:"timestamp"`
	}
	// A payload that is not JSON still gets a stamp: the account is what matters.
	b, _ := io.ReadAll(io.LimitReader(in, maxHookPayload))
	_ = json.Unmarshal(b, &payload)

	id, ok := accounts.ReadIdentity(claudeJSON, false)
	if !ok {
		return "no signed-in account could be read from " + claudeJSON
	}
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return err.Error()
		}
		dir = filepath.Join(base, "agentscope")
	}
	at := stampNow().UTC()
	if t, err := time.Parse(time.RFC3339Nano, payload.Timestamp); err == nil {
		at = t.UTC()
	}
	if len(payload.SessionID) > 64 {
		payload.SessionID = payload.SessionID[:64]
	}
	_, err := accounts.NewStamps(dir).Append(accounts.Stamp{At: at, SessionID: payload.SessionID, AccountHash: id.Key, Label: id.Label, Plan: id.Plan})
	if err != nil {
		return err.Error()
	}
	return ""
}
