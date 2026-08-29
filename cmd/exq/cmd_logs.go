package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/ystsbry/exq/internal/daemon"
)

// pollInterval is how often -f looks for more output. Job logs are
// written by a separate process, so there is nothing to wait on but the
// file itself.
const pollInterval = 200 * time.Millisecond

func newLogsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs <job-id>",
		Short: "Show the output of a background job",
		Long: `Print what a background job wrote to stdout and stderr, combined in the
order exqd received it. With -f the output is followed until the job
finishes, like tail -f.

Job logs are files under the exq state directory, so this works whether
or not exqd is currently running.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			// Reading the record first turns a typo into "job not found"
			// instead of a bare missing-file error.
			if _, err := daemon.ReadJobRecord(id); err != nil {
				return err
			}
			return writeJobLog(cmd.OutOrStdout(), id, follow)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing new output until the job finishes")
	return cmd
}

// writeJobLog copies a job's log to out, optionally following it until
// the job reaches a terminal state.
func writeJobLog(out io.Writer, id string, follow bool) error {
	f, err := openJobLog(id, follow)
	if err != nil {
		return err
	}
	if f == nil {
		fmt.Fprintln(out, "(no output)")
		return nil
	}
	defer func() { _ = f.Close() }()

	for {
		n, err := io.Copy(out, f)
		if err != nil {
			return err
		}
		if !follow {
			return nil
		}
		if n > 0 {
			continue
		}
		if jobFinished(id) {
			// One last pass: the job may have written its final lines
			// between the copy above and the state check.
			_, err := io.Copy(out, f)
			return err
		}
		time.Sleep(pollInterval)
	}
}

// openJobLog opens a job's log file. A queued job has no log yet; when
// following, wait for it to appear rather than reporting nothing.
// Returns a nil file when there is genuinely no output to show.
func openJobLog(id string, follow bool) (*os.File, error) {
	path := daemon.JobLogPath(id)
	for {
		f, err := os.Open(path)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if !follow || jobFinished(id) {
			return nil, nil
		}
		time.Sleep(pollInterval)
	}
}

// jobFinished reports whether the job has reached a terminal state. An
// unreadable record counts as finished: there is nothing left to follow.
func jobFinished(id string) bool {
	info, err := daemon.ReadJobRecord(id)
	if err != nil {
		return true
	}
	return info.State.Done()
}
