package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ystsbry/exq/internal/herdr"
)

func newRunCmd() *cobra.Command {
	var (
		background bool
		scheduleID string
	)
	cmd := &cobra.Command{
		Use:   "run <name> [-- <values...>]",
		Short: "Run a command by name",
		Long: `Run a command by name. Values after "--" are passed to run.sh as
positional arguments ($1, $2, …) in the order the command's [[args]] are
declared in command.toml.

With --bg the command is handed to the exqd daemon instead of running
here: exq prints a job id and returns immediately, and the job keeps
running after this terminal is gone. Follow it with "exq logs <job-id>".`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, values, err := splitRunArgs(args, cmd.ArgsLenAtDash())
			if err != nil {
				return err
			}
			st, err := openStoreFromWD()
			if err != nil {
				return err
			}
			// Resolve the command here too, even for a background job that
			// exqd resolves again at execution time: a typo should fail in
			// front of the user, not silently in a job record.
			c, err := st.Get(name)
			if err != nil {
				return err
			}
			if background {
				spec, err := jobSpecFromWD(c.Name, values)
				if err != nil {
					return err
				}
				spec.ScheduleID = scheduleID
				return submitJob(cmd.OutOrStdout(), spec)
			}
			return execute(cmd.OutOrStdout(), cmd.ErrOrStderr(), st, c, values, herdr.New())
		},
	}
	cmd.Flags().BoolVar(&background, "bg", false, "submit the command to exqd and return immediately")
	cmd.Flags().StringVar(&scheduleID, "schedule-id", "",
		"mark the job as coming from this schedule (set by the timer units `exq schedule add` generates)")
	return cmd
}

// splitRunArgs separates the command name from the values that follow "--".
// dash is cobra's ArgsLenAtDash: the number of args before "--", or -1 when
// no "--" was given. Extra positionals without "--" are rejected with a hint
// so `exq run x prod` fails clearly instead of silently ignoring "prod".
func splitRunArgs(args []string, dash int) (name string, values []string, err error) {
	switch {
	case dash < 0:
		if len(args) > 1 {
			return "", nil, fmt.Errorf(
				"unexpected arguments %v — pass command arguments after \"--\": exq run %s -- %v",
				args[1:], args[0], args[1])
		}
		return args[0], nil, nil
	case dash == 1:
		return args[0], args[1:], nil
	default:
		return "", nil, fmt.Errorf("expected exactly one command name before \"--\", got %d", dash)
	}
}
