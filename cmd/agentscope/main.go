// agentscope is a live graphical view of the Claude Code agent network on this
// machine: which sessions and sub-agents exist, what each is doing, which
// repository each holds, and the messages between them.
//
//	agentscope snapshot [--json]    print one snapshot (terminal use, tests)
//	agentscope serve                serve the live view on 127.0.0.1
//
// Everything is read from ~/.claude and the local clones, read-only, and only
// metadata is extracted. Nothing leaves the machine.
package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/go-agentsync/agentscope/collect"
	"github.com/spf13/cobra"
)

// osExit allows tests to override os.Exit.
var osExit = os.Exit

func main() {
	if err := RootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		osExit(1)
	}
}

// version is the module version, "devel" outside a release build.
func version() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "devel"
}

// common holds the flags shared by the sub-commands.
type common struct {
	claudeDir, localhost, github string
	active, horizon, tellWindow  time.Duration
	maxAgents                    int
}

func (c *common) bind(cmd *cobra.Command) {
	def, _ := collect.DefaultOptions()
	f := cmd.Flags()
	f.StringVar(&c.claudeDir, "claude-dir", def.ClaudeDir, "the Claude Code data directory (read-only)")
	f.StringVar(&c.localhost, "localhost-root", def.LocalhostRoot, "directory of <org>-<repo>[-topic] clones")
	f.StringVar(&c.github, "github-root", def.GithubRoot, "directory of <org>/<repo> clones")
	f.DurationVar(&c.active, "active", def.Active, "a session is active when its transcript was written within this")
	f.DurationVar(&c.horizon, "horizon", def.Horizon, "sessions and expired leases older than this are not listed")
	f.DurationVar(&c.tellWindow, "tell-window", def.TellWindow, "tells older than this are not listed")
	f.IntVar(&c.maxAgents, "max-agents", def.MaxAgents, "sub-agents listed per session, newest first")
}

func (c *common) options() (collect.Options, error) {
	o, err := collect.DefaultOptions()
	if err != nil {
		return o, err
	}
	o.ClaudeDir, o.LocalhostRoot, o.GithubRoot = c.claudeDir, c.localhost, c.github
	o.Active, o.Horizon, o.TellWindow, o.MaxAgents = c.active, c.horizon, c.tellWindow, c.maxAgents
	if o.Active <= 0 || o.Horizon <= 0 || o.TellWindow < 0 || o.MaxAgents < 0 {
		return o, fmt.Errorf("--active and --horizon must be positive, --tell-window and --max-agents not negative")
	}
	return o, nil
}

// RootCmd returns the top-level cobra command.
func RootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "agentscope",
		Short:        "Live graphical view of the Claude Code agent network on this machine",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE:         func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	root.AddCommand(snapshotCmd(), serveCmd(), &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, _ []string) { fmt.Fprintln(cmd.OutOrStdout(), version()) },
	})
	return root
}
