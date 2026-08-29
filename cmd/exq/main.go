package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ystsbry/exq/internal/command"
	"github.com/ystsbry/exq/internal/herdr"
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

// exitError asks main for a specific exit status without any further
// output: whoever returned it has already reported the failure in its own
// words (the ✗ frame, the workflow summary), and a second "Error: …" line
// would only repeat it.
type exitError struct {
	code int
}

func (e exitError) Error() string {
	return fmt.Sprintf("exit status %d", e.code)
}

// main is the only place that calls os.Exit, so deferred cleanup anywhere
// in the command tree still runs before the process ends.
func main() {
	if err := newRootCmd().Execute(); err != nil {
		if exit, ok := errors.AsType[exitError](err); ok {
			os.Exit(exit.code)
		}
		fmt.Fprintln(os.Stderr, "Error:", err)
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
		SilenceUsage: true,
		// main reports errors itself so it can let an exitError through
		// silently — the command has already said what went wrong.
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newRunCmd())
	cmd.AddCommand(newRemoveCmd())
	cmd.AddCommand(newDaemonCmd())
	cmd.AddCommand(newScheduleCmd())
	cmd.AddCommand(newJobsCmd())
	cmd.AddCommand(newLogsCmd())
	cmd.AddCommand(newStopCmd())
	cmd.AddCommand(newDemoCmd())
	return cmd
}

// runTUI shows the command list and executes the picked command after the
// TUI has released the terminal. While the TUI is open the pane reports
// itself to herdr as idle; quitting without running anything releases the
// agent row so nothing lingers in the sidebar.
func runTUI(out, errOut io.Writer) error {
	st, err := openStoreFromWD()
	if err != nil {
		return err
	}
	rep := herdr.New()
	rep.Report(herdr.StateIdle, "", "")
	res, err := tui.Run(st, tui.Deps{Jobs: daemonClient()})
	if err != nil {
		rep.Release()
		return err
	}
	if res == nil {
		rep.Release()
		return nil
	}
	return execute(out, errOut, st, res.Command, res.Values, rep)
}

// execute runs one picked command, dispatching on its kind. Scripts and
// workflows report progress differently, so the two paths stay separate
// past this point.
func execute(out, errOut io.Writer, st *store.Store, c command.Command, values []string, rep *herdr.Reporter) error {
	if c.Kind == command.KindWorkflow {
		return executeWorkflow(out, st, c, values, rep)
	}
	return executeScript(errOut, st, c, values, rep)
}

// executeScript runs a script with a frame around its raw output
// (▶ name … ✓/✗ name), so what ran and how it ended is always visible —
// including after the TUI has restored the terminal. The frame goes to
// errOut (stderr in normal use) so piping the script's stdout stays clean.
// A non-zero script comes back as an exitError carrying its code. The
// herdr reporter sees working while the script runs and idle afterwards.
func executeScript(errOut io.Writer, st *store.Store, c command.Command, values []string, rep *herdr.Reporter) error {
	label := c.Name
	if len(values) > 0 {
		label += " " + strings.Join(values, " ")
	}
	rep.Report(herdr.StateWorking, label, "")
	fmt.Fprintf(errOut, "▶ %s\n", label)

	start := time.Now()
	code, err := runner.Run(c, st.Root, values)
	dur := time.Since(start).Seconds()
	rep.Report(herdr.StateIdle, label, "")

	if err != nil {
		return err
	}
	if code != 0 {
		fmt.Fprintf(errOut, "✗ %s (%.1fs, exit %d)\n", c.Name, dur, code)
		return exitError{code: code}
	}
	fmt.Fprintf(errOut, "✓ %s (%.1fs)\n", c.Name, dur)
	return nil
}

// executeWorkflow runs a workflow with progress and the per-step summary
// on out. A failing step comes back as an exitError carrying that step's
// code; pre-flight validation failures are returned as a plain error. The
// herdr reporter mirrors the run: working with per-step progress in the
// custom status, then idle once it is over.
func executeWorkflow(out io.Writer, st *store.Store, c command.Command, values []string, rep *herdr.Reporter) error {
	rep.Report(herdr.StateWorking, c.Name, "")
	res, err := workflow.Run(context.Background(), st, c, workflow.Options{
		Workdir:  st.Root,
		Values:   values,
		Progress: out,
		OnStep: func(current, total int, name string) {
			rep.Report(herdr.StateWorking, c.Name, fmt.Sprintf("step %d/%d %s", current, total, name))
		},
	})
	rep.Report(herdr.StateIdle, c.Name, "")
	if err != nil {
		return err
	}
	if code := workflow.Report(out, c, res); code != 0 {
		return exitError{code: code}
	}
	return nil
}

// openStore opens the store rooted at dir, failing early with a hint when
// exq init has not been run yet.
func openStore(dir string) (*store.Store, error) {
	st, err := store.Open(dir)
	if err != nil {
		return nil, err
	}
	if !st.Exists() {
		return nil, fmt.Errorf("%s not found — run `exq init` first", st.Dir())
	}
	return st, nil
}

// openStoreFromWD opens the store rooted at the directory exq was invoked
// from, which is where commands are looked up and run.
func openStoreFromWD() (*store.Store, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return openStore(wd)
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
