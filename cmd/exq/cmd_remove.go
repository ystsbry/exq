package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

func newRemoveCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm"},
		Short:   "Delete a command",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			c, err := st.Get(args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if !yes && !confirmYes(cmd.InOrStdin(), out, c.Name) {
				fmt.Fprintln(out, "Cancelled.")
				return nil
			}
			if err := st.Remove(c.Name); err != nil {
				return err
			}
			fmt.Fprintf(out, "Removed %s\n", c.Name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return cmd
}

func confirmYes(in io.Reader, out io.Writer, name string) bool {
	fmt.Fprintf(out, "Delete %q? [y/N]: ", name)
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return false
	}
	resp := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return resp == "y" || resp == "yes"
}
