package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ystsbry/exq/internal/command"
	"github.com/ystsbry/exq/internal/runner"
	"github.com/ystsbry/exq/internal/store"
	"github.com/ystsbry/exq/internal/tui"
	"github.com/ystsbry/exq/internal/workflow"
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
	if res.Command.Kind == command.KindWorkflow {
		return executeWorkflow(st, res.Command, res.Values)
	}
	return executeScript(st, res.Command, res.Values)
}

// executeScript runs a script with a stderr frame around its raw output
// (▶ name … ✓/✗ name), so what ran and how it ended is always visible —
// including after the TUI has restored the terminal. The frame goes to
// stderr so piping the script's stdout stays clean. A non-zero script
// exits exq with that code.
func executeScript(st *store.Store, c command.Command, values []string) error {
	label := c.Name
	if len(values) > 0 {
		label += " " + strings.Join(values, " ")
	}
	fmt.Fprintf(os.Stderr, "▶ %s\n", label)
	start := time.Now()
	code, err := runner.Run(c, st.Root, values)
	if err != nil {
		return err
	}
	dur := time.Since(start).Seconds()
	if code != 0 {
		fmt.Fprintf(os.Stderr, "✗ %s (%.1fs, exit %d)\n", c.Name, dur, code)
		os.Exit(code)
	}
	fmt.Fprintf(os.Stderr, "✓ %s (%.1fs)\n", c.Name, dur)
	return nil
}

// executeWorkflow runs a workflow with progress on stdout and prints the
// per-step summary. A failing step makes exq exit with that step's exit
// code; pre-flight validation failures are returned as a plain error.
func executeWorkflow(st *store.Store, c command.Command, values []string) error {
	res, err := workflow.Run(st, c, st.Root, values, os.Stdout)
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Print(workflow.Summary(res))
	fmt.Println()
	if f := res.Failed(); f != nil {
		fmt.Printf("workflow %s failed at step %s\n", c.Name, f.Name)
		code := f.ExitCode
		if code <= 0 {
			code = 1
		}
		os.Exit(code)
	}
	fmt.Printf("workflow %s: all %d steps succeeded\n", c.Name, len(res.Steps))
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
