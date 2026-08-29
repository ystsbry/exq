package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <job-id>",
		Short: "Stop a running background job",
		Long: `Ask exqd to terminate a running job: SIGTERM to the job's process group,
then SIGKILL if it is still around after a short grace period.

Stopping a job that already finished is not an error — it just reports
how the job ended.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := daemonClient().Stop(args[0])
			if err != nil {
				return daemonHint(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "job %s: %s\n", info.ID, jobOutcome(*info))
			return nil
		},
	}
}
