package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ystsbry/exq/internal/runner"
)

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <name> [-- <values...>]",
		Short: "Run a command by name",
		Long: `Run a command by name. Values after "--" are passed to run.sh as
positional arguments ($1, $2, …) in the order the command's [[args]] are
declared in command.toml.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, values, err := splitRunArgs(args, cmd.ArgsLenAtDash())
			if err != nil {
				return err
			}
			st, err := openStore()
			if err != nil {
				return err
			}
			c, err := st.Get(name)
			if err != nil {
				return err
			}
			if err := ensureExecutable(c); err != nil {
				return err
			}
			code, err := runner.Run(c, st.Root, values)
			if err != nil {
				return err
			}
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
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
