package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-agentsync/agentscope/collect"
	"github.com/go-agentsync/agentscope/quota"
	"github.com/go-agentsync/agentscope/serve"
	"github.com/go-agentsync/agentscope/usage"
	"github.com/go-agentsync/agentscope/webui"
	"github.com/go-agentsync/agentscope/wire"
	"github.com/spf13/cobra"
)

// quotaRunner reads the official usage; tests replace it.
var quotaRunner quota.Runner = quota.Exec

func serveCmd() *cobra.Command {
	var (
		c        common
		addr     string
		web      string
		cacheDir string
		history  string
		interval time.Duration
		showOrg  bool
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the live view on 127.0.0.1",
		Long: `serve collects the network every --interval and serves the browser UI and a
gRPC stream of snapshots (over a WebSocket, so the wasm UI speaks the same
gRPC a terminal client would) on a loopback address.

The address must be a loopback IP; every request must carry a Host header naming
it (DNS-rebinding defence); the only methods are GET and HEAD.

The browser UI (wasm) is built on first run from the sources that belong to this
binary, with the local Go toolchain, into the user cache directory, once per
version and toolchain. --web DIR serves a directory you built instead and needs
no Go toolchain.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o, err := c.options()
			if err != nil {
				return err
			}
			if interval < 500*time.Millisecond {
				return fmt.Errorf("--interval must be at least 500ms")
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			dir := web
			if dir == "" {
				if dir, err = webui.Ensure(ctx, webui.Options{CacheDir: cacheDir, Out: cmd.OutOrStdout()}); err != nil {
					return err
				}
			}
			if dir, err = filepath.Abs(dir); err != nil {
				return err
			}
			if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
				return fmt.Errorf("no UI in %s: %w", dir, err)
			}
			ui, source := os.DirFS(dir), "the UI in "+dir
			ucache, uconf, err := usageDirs(cacheDir, history)
			if err != nil {
				return err
			}
			eng := usage.NewEngine(o.ClaudeDir, ucache, uconf, quotaRunner)
			if showOrg {
				eng.ShowOrg(o.ClaudeDir)
			}
			hub := wire.NewHub(usage.Source{Inner: collect.New(o), Eng: eng}, interval, 10*time.Second)
			hub.SetRenamer(eng)
			srv, err := serve.New(serve.Config{Addr: addr, Web: ui, Scope: hub})
			if err != nil {
				return err
			}
			go hub.Run(ctx)
			go eng.Run(ctx)
			fmt.Fprintf(cmd.OutOrStdout(), "agentscope %s serving %s (%s); Ctrl-C to stop\n", version(), srv.URL(), source)
			return srv.Serve(ctx)
		},
	}
	c.bind(cmd)
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8765", "loopback address to listen on (port 0 picks one)")
	cmd.Flags().StringVar(&web, "web", "", "serve the UI from this directory instead of building it on first run")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "where the built UI is kept (default <user cache dir>/agentscope)")
	cmd.Flags().StringVar(&history, "history-dir", "", "where the official usage readings are recorded (default <user config dir>/agentscope)")
	cmd.Flags().BoolVar(&showOrg, "show-org", false, "offer the organisation's name (from ~/.claude.json) as the suggested value of the Rename dialog; off, no organisation name is read or sent")
	cmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "how often the network is collected")
	return cmd
}

// usageDirs are where the Quota tab keeps what it learns: the transcript sums in
// the cache directory (rebuildable), the recorded official readings in the
// config directory (not rebuildable, so not a cache).
func usageDirs(cacheDir, history string) (cache, config string, err error) {
	if cacheDir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return "", "", err
		}
		cacheDir = filepath.Join(base, "agentscope")
	}
	if history == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", "", err
		}
		history = filepath.Join(base, "agentscope")
	}
	return cacheDir, history, nil
}
