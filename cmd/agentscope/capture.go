package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-agentsync/agentscope/demo"
	"github.com/spf13/cobra"
)

// captureCmd renders a synthetic, deterministic scenario of the screen into one
// animated SVG that scales without loss, or into a WebM video of the same frames.
func captureCmd() *cobra.Command {
	var scenario, out, size, format string
	var quality int
	var list bool
	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Render a synthetic demo scenario of the screen into an animated SVG or a WebM",
		Long: "Plays a scenario of invented accounts, sessions and repositories, with a fixed clock,\n" +
			"through the same view-model as the live screen, and writes one animated SVG, or\n" +
			"a WebM (VP8) video of the same frames for the platforms that take video only.\n" +
			"Nothing is read from this machine; the same scenario gives the same bytes.\n" +
			"The default --out is a directory under the user's config directory, and a path\n" +
			"inside a git work tree is refused. The format is --format, or the extension of\n" +
			"--out; the two must agree. A WebM takes any size: the encoder pads to whole blocks itself.",
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
			fm, err := demo.ResolveFormat(format, out)
			if err != nil {
				return err
			}
			if fm != demo.FormatWebM && cmd.Flags().Changed("quality") {
				return fmt.Errorf("--quality applies to --format webm only")
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
				out = filepath.Join(dir, sanitize(scenario)+"."+fm)
			}
			o := demo.Options{W: width, H: height}
			if fm == demo.FormatWebM {
				r, err := demo.WriteWebM(scenario, out, o, quality)
				if err != nil {
					return err
				}
				fmt.Fprintf(w, "wrote %s: %d bytes, %d slots, %s, VP8 quantizer %d, %dx%d\n",
					out, len(r.Bytes), len(r.Doc.Slots()), r.Duration, r.Quality, width, height)
				return nil
			}
			r, err := demo.Write(scenario, out, o)
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
	f.StringVar(&out, "out", "", "the file to write (default: <user config dir>/agentscope/captures/<scenario>.<format>)")
	f.StringVar(&format, "format", "", "svg or webm (default: from the --out extension, else svg)")
	f.IntVar(&quality, "quality", 0, "webm only: the VP8 quantizer, 1 (best) to 63 (worst); 0 is the default, 10")
	f.StringVar(&size, "size", demo.DefaultSize, "the captured window, WIDTHxHEIGHT")
	f.BoolVar(&list, "list", false, "list the scenarios and exit")
	return cmd
}

// sanitize keeps a scenario name usable as a file name; Lookup refuses anything
// that is not a known name before it is written.
func sanitize(s string) string { return strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(s) }
