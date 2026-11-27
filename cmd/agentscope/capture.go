package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-agentsync/agentscope/demo"
	"github.com/spf13/cobra"
)

// captureCmd renders a synthetic, deterministic scenario of the screen into one
// animated SVG that scales without loss.
func captureCmd() *cobra.Command {
	var scenario, out, size string
	var list bool
	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Render a synthetic demo scenario of the screen into one animated SVG",
		Long: "Plays a scenario of invented accounts, sessions and repositories, with a fixed clock,\n" +
			"through the same view-model as the live screen, and writes one animated SVG.\n" +
			"Nothing is read from this machine; the same scenario gives the same bytes.\n" +
			"The default --out is a directory under the user's config directory, and a path\n" +
			"inside a git work tree is refused.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			if list {
				for _, n := range demo.Names() {
					sc, _ := demo.Lookup(n)
					fmt.Fprintf(w, "%s\t%s\n", n, sc.Title)
				}
				return nil
			}
			width, height, err := demo.ParseSize(size)
			if err != nil {
				return err
			}
			if out == "" {
				dir, err := demo.DefaultOutDir()
				if err != nil {
					return err
				}
				out = filepath.Join(dir, sanitize(scenario)+".svg")
			}
			r, err := demo.Write(scenario, out, demo.Options{W: width, H: height})
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "wrote %s: %d bytes, %d frames, %d pictures, %s, %d raster elements\n",
				out, len(r.SVG), r.Frames, r.Stats.Pictures, r.Duration, r.Raster)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&scenario, "scenario", "overview", "the scenario to play (see --list)")
	f.StringVar(&out, "out", "", "the SVG to write (default: <user config dir>/agentscope/captures/<scenario>.svg)")
	f.StringVar(&size, "size", demo.DefaultSize, "the captured window, WIDTHxHEIGHT")
	f.BoolVar(&list, "list", false, "list the scenarios and exit")
	return cmd
}

// sanitize keeps a scenario name usable as a file name; Lookup refuses anything
// that is not a known name before it is written.
func sanitize(s string) string { return strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(s) }
