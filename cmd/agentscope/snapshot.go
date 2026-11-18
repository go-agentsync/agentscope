package main

import (
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/go-agentsync/agentscope/collect"
	"github.com/go-agentsync/agentscope/model"
	"github.com/spf13/cobra"
)

func snapshotCmd() *cobra.Command {
	var (
		c      common
		asJSON bool
		stats  bool
		warm   int
	)
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Print one snapshot of the agent network",
		Long: `snapshot collects the network once and prints it: sessions with their
sub-agents, the repositories they hold, the tells between them, and the
anomalies met on the way (a lease key that is no repository, a lease whose
owner has no transcript, ...). With --json the output is the exact message the
browser receives.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o, err := c.options()
			if err != nil {
				return err
			}
			col := collect.New(o)
			var m0, m1 runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&m0)
			t0 := time.Now()
			snap := col.Snapshot()
			cold := time.Since(t0)
			runtime.ReadMemStats(&m1)
			allocs, bytes := m1.Mallocs-m0.Mallocs, m1.TotalAlloc-m0.TotalAlloc
			if stats {
				fmt.Fprintf(cmd.ErrOrStderr(), "cold snapshot: %s, %d allocs, %d bytes allocated\n", cold.Round(time.Microsecond), allocs, bytes)
				for i := 0; i < warm; i++ {
					runtime.ReadMemStats(&m0)
					t1 := time.Now()
					snap = col.Snapshot()
					el := time.Since(t1)
					runtime.ReadMemStats(&m1)
					fmt.Fprintf(cmd.ErrOrStderr(), "warm snapshot: %s, %d allocs, %d bytes allocated\n", el.Round(time.Microsecond), m1.Mallocs-m0.Mallocs, m1.TotalAlloc-m0.TotalAlloc)
				}
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(snap)
			}
			render(cmd.OutOrStdout(), snap)
			return nil
		},
	}
	c.bind(cmd)
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the snapshot as JSON")
	cmd.Flags().BoolVar(&stats, "stats", false, "print time and allocation counts of the collection to stderr")
	cmd.Flags().IntVar(&warm, "warm", 0, "with --stats, also measure this many further (incremental) snapshots")
	return cmd
}

// render prints a snapshot for a terminal.
func render(w io.Writer, s model.Snapshot) {
	c := s.Counts()
	fmt.Fprintf(w, "agentscope %s: %d sessions (%d active in %s), %d sub-agents, %d repos, %d leases (%d held), %d tells, %d anomalies\n",
		s.At.Format("2006-01-02 15:04:05"), c.Sessions, c.Active, s.ActiveFor, c.Agents, c.Repos, c.Leases, c.Held, c.Tells, c.Anomalies)
	fmt.Fprintf(w, "\nSESSIONS\n")
	for _, se := range s.Sessions {
		state := "idle  "
		if se.Active {
			state = "ACTIVE"
		}
		if !se.HasTranscript {
			state = "no-tr "
		}
		repo := se.CwdRepo
		var held []string
		for _, l := range s.LeasesOf(se.ID) {
			if !l.Expired {
				held = append(held, l.Repo)
			}
		}
		if len(held) > 0 {
			repo = strings.Join(held, ",")
		}
		fmt.Fprintf(w, "  %s %s %-34s agents=%-3d %s\n", se.Short, state, model.Truncate(repo, 34), len(se.Agents), model.Truncate(s.Task(se), 70))
	}
	fmt.Fprintf(w, "\nLEASES (%d listed)\n", len(s.Leases))
	for _, l := range s.Leases {
		st := "held   "
		if l.Expired {
			st = "expired"
		}
		fmt.Fprintf(w, "  %s %-8s %-40s %s\n", st, model.ShortID(l.Owner), model.Truncate(l.Repo, 40), model.Truncate(l.Note, 60))
	}
	fmt.Fprintf(w, "\nTELLS (%d)\n", len(s.Tells))
	for _, t := range s.Tells {
		fmt.Fprintf(w, "  %s %s -> %s: %s\n", t.At.Format("01-02 15:04"), model.ShortID(t.From), t.Target, model.Truncate(t.Text, 80))
	}
	fmt.Fprintf(w, "\nANOMALIES (%d)\n", len(s.Anomalies))
	for _, a := range s.Anomalies {
		fmt.Fprintf(w, "  %s: %s %s\n", a.Kind, a.Subject, a.Detail)
	}
}
