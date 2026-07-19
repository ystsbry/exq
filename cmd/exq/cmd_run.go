package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/ystsbry/exq/internal/runner"
)

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <name>",
		Short: "Run a command by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			c, err := st.Get(args[0])
			if err != nil {
				return err
			}
			code, err := runner.Run(c, st.Root)
			if err != nil {
				return err
			}
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
}
