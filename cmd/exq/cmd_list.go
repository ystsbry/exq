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
			st, err := openStoreFromWD()
			if err != nil {
				return err
			}
			cmds, err := st.List()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(cmds) == 0 {
				fmt.Fprintf(out, "No commands under %s.\n", st.Dir())
				return nil
			}
			width := 0
			for _, c := range cmds {
				width = max(width, len(c.Name))
			}
			// cmds arrive kind-major from the store: scripts first, then
			// workflows, each under its own section header.
			for i, c := range cmds {
				if i == 0 || c.Kind != cmds[i-1].Kind {
					if i > 0 {
						fmt.Fprintln(out)
					}
					fmt.Fprintf(out, "%s:\n", c.Kind)
				}
				fmt.Fprintf(out, "  %-*s  %s\n", width, c.Name, c.Meta())
			}
			return nil
		},
	}
}
