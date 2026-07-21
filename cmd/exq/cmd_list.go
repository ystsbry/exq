package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ystsbry/exq/internal/command"
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
				fmt.Fprintf(out, "No commands under %s.\n", st.Dir())
				return nil
			}
			width := 0
			for _, c := range cmds {
				if len(c.Name) > width {
					width = len(c.Name)
				}
			}
			// cmds arrive kind-major from the store: scripts first, then
			// workflows, each under its own section header.
			for i, c := range cmds {
				if i == 0 || c.Kind != cmds[i-1].Kind {
					if i > 0 {
						fmt.Fprintln(out)
					}
					label := "scripts"
					if c.Kind == command.KindWorkflow {
						label = "workflows"
					}
					fmt.Fprintf(out, "%s:\n", label)
				}
				meta := c.Description
				if c.Kind == command.KindWorkflow && len(c.Steps) > 0 {
					meta = strings.TrimSpace(meta + " (steps: " + strings.Join(c.Steps, " → ") + ")")
				} else if len(c.Args) > 0 {
					keys := make([]string, len(c.Args))
					for i, a := range c.Args {
						keys[i] = a.Key
					}
					meta = strings.TrimSpace(meta + " (args: " + strings.Join(keys, ", ") + ")")
				}
				fmt.Fprintf(out, "  %-*s  %s\n", width, c.Name, meta)
			}
			return nil
		},
	}
}
