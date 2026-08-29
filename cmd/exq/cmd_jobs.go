package main

import (
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/ystsbry/exq/internal/daemon"
)

func newJobsCmd() *cobra.Command {
	var prune bool
	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "List background jobs",
		Long: `List the background jobs exqd knows about, newest first: their state,
when they started and how long they took (so far, for a running one).

Jobs are recorded per user, not per project, so this shows the jobs
submitted from every directory.

With --prune, finished jobs are deleted instead of listed: their records
and logs are removed and the freed space is reported. Running jobs are
left alone. Nothing is rotated automatically, so this is how a long-lived
job history is kept in check.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if prune {
				return pruneJobs(out)
			}
			jobs, err := daemonClient().List()
			if err != nil {
				return daemonHint(err)
			}
			if len(jobs) == 0 {
				fmt.Fprintln(out, "No jobs yet — submit one with `exq run <name> --bg`.")
				return nil
			}
			writeJobTable(out, jobs, time.Now())
			return nil
		},
	}
	cmd.Flags().BoolVar(&prune, "prune", false, "delete the records and logs of finished jobs")
	return cmd
}

// pruneJobs deletes finished job directories and reports the result.
func pruneJobs(out io.Writer) error {
	res, err := daemon.PruneJobs()
	if err != nil {
		return err
	}
	if res.Removed == 0 {
		fmt.Fprintln(out, "Nothing to prune — no finished jobs.")
	} else {
		fmt.Fprintf(out, "Removed %s, freeing %s.\n", plural(res.Removed, "job"), humanBytes(res.Freed))
	}
	if res.Kept > 0 {
		fmt.Fprintf(out, "Kept %s still running.\n", plural(res.Kept, "job"))
	}
	return nil
}

// plural renders a count with its noun, in the singular where it fits.
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// humanBytes renders a byte count in the unit that keeps it readable.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for size := n / unit; size >= unit; size /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
