package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ystsbry/exq/internal/store"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create ./.exq and exclude it via .git/info/exclude",
		Long: `Create the .exq/commands directory in the current working directory and
append ".exq/" to the repository's .git/info/exclude so the directory never
appears in git status. Safe to re-run: nothing is duplicated.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			st, err := store.Open(wd)
			if err != nil {
				return err
			}
			res, err := st.Init()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if res.CreatedDir {
				fmt.Fprintf(out, "Created %s\n", st.CommandsDir())
			} else {
				fmt.Fprintf(out, "%s already exists\n", st.Dir())
			}
			if res.UpdatedExclude {
				fmt.Fprintf(out, "Added \".exq/\" to %s\n", res.ExcludePath)
			} else {
				fmt.Fprintf(out, "%s already excludes \".exq/\"\n", res.ExcludePath)
			}
			return nil
		},
	}
}
