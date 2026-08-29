package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func newJobsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "jobs",
		Short: "List background jobs",
		Long: `List the background jobs exqd knows about, newest first: their state,
when they started and how long they took (so far, for a running one).

Jobs are recorded per user, not per project, so this shows the jobs
submitted from every directory.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			jobs, err := daemonClient().List()
			if err != nil {
				return daemonHint(err)
			}
			out := cmd.OutOrStdout()
			if len(jobs) == 0 {
				fmt.Fprintln(out, "No jobs yet — submit one with `exq run <name> --bg`.")
				return nil
			}
			writeJobTable(out, jobs, time.Now())
			return nil
		},
	}
}
