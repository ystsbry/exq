package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List available commands",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			cmds, err := st.List()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(cmds) == 0 {
				fmt.Fprintf(out, "No commands under %s.\n", st.CommandsDir())
				return nil
			}
			width := 0
			for _, c := range cmds {
				if len(c.Name) > width {
					width = len(c.Name)
				}
			}
			for _, c := range cmds {
				fmt.Fprintf(out, "%-*s  %s\n", width, c.Name, c.Description)
			}
			return nil
		},
	}
}
