package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-agentsync/agentscope/collect"
	"github.com/go-agentsync/agentscope/serve"
	"github.com/go-agentsync/agentscope/webui"
	"github.com/go-agentsync/agentscope/wire"
	"github.com/spf13/cobra"
)

func serveCmd() *cobra.Command {
	var (
		c        common
		addr     string
		web      string
		cacheDir string
		interval time.Duration
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
			hub := wire.NewHub(collect.New(o), interval, 10*time.Second)
			srv, err := serve.New(serve.Config{Addr: addr, Web: ui, Scope: hub})
			if err != nil {
				return err
			}
			go hub.Run(ctx)
			fmt.Fprintf(cmd.OutOrStdout(), "agentscope %s serving %s (%s); Ctrl-C to stop\n", version(), srv.URL(), source)
			return srv.Serve(ctx)
		},
	}
	c.bind(cmd)
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8765", "loopback address to listen on (port 0 picks one)")
	cmd.Flags().StringVar(&web, "web", "", "serve the UI from this directory instead of building it on first run")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "where the built UI is kept (default <user cache dir>/agentscope)")
	cmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "how often the network is collected")
	return cmd
}
