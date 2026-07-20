package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ystsbry/exq/internal/runner"
	"github.com/ystsbry/exq/internal/store"
	"github.com/ystsbry/exq/internal/tui"
)

var (
	version = "0.1.0-dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exq",
		Short: "Manage and run local-only commands kept in ./.exq (git-excluded)",
		Long: `exq manages commands that live in the .exq directory of the current
working directory. The directory is excluded from git via .git/info/exclude,
so the commands stay local to your machine and never show up in the repo.

Running exq with no arguments opens the interactive TUI: pick a command to
run it, or delete one with "d".`,
		SilenceUsage:  true,
		SilenceErrors: false,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
	}
	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newRunCmd())
	cmd.AddCommand(newRemoveCmd())
	cmd.AddCommand(newDemoCmd())
	return cmd
}

// runTUI shows the command list and executes the picked command after the
// TUI has released the terminal.
func runTUI() error {
	st, err := openStore()
	if err != nil {
		return err
	}
	res, err := tui.Run(st)
	if err != nil {
		return err
	}
	if res == nil {
		return nil
	}
	code, err := runner.Run(res.Command, st.Root, res.Values)
	if err != nil {
		return err
	}
	if code != 0 {
		os.Exit(code)
	}
	return nil
}

// openStore opens the store rooted at the cwd, failing early with a hint
// when exq init has not been run yet.
func openStore() (*store.Store, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	st, err := store.Open(wd)
	if err != nil {
		return nil, err
	}
	if !st.Exists() {
		return nil, fmt.Errorf("%s not found — run `exq init` first", st.Dir())
	}
	return st, nil
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print exq version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "exq %s (commit %s, built %s)\n", version, commit, date)
			return nil
		},
	}
}
