// Package runner executes exq commands, wiring the entrypoint to the
// user's terminal — or, for a background job, to a daemon-owned log file
// — and reporting the exit code.
package runner

import (
	"cmp"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/ystsbry/exq/internal/command"
)

// stopGrace is how long a cancelled command has to wind down after the
// SIGTERM before it is killed outright. It bounds `exq stop`: a job that
// ignores the polite request is gone within this window.
const stopGrace = 5 * time.Second

// Options configures one execution beyond the command and its arguments.
// The zero value wires the child to exq's own streams, which is what a
// synchronous `exq run` in a terminal wants.
type Options struct {
	Stdin  io.Reader // nil: os.Stdin
	Stdout io.Writer // nil: os.Stdout
	Stderr io.Writer // nil: os.Stderr
	// Group puts the child in a process group of its own and makes a
	// cancelled context signal that whole group — SIGTERM first, SIGKILL
	// after stopGrace — so stopping a background job takes the processes
	// the entrypoint spawned with it instead of orphaning them.
	Group bool
}

// Run executes c's entrypoint with the working directory set to workdir
// (the directory the user invoked exq from, not the command directory),
// wired to exq's own terminal.
func Run(c command.Command, workdir string, args []string) (int, error) {
	return RunWith(context.Background(), c, workdir, args, Options{})
}

// RunWith executes c's entrypoint with the working directory set to
// workdir. args are passed through verbatim as $1, $2, … — no shell is
// involved, so values may contain spaces or metacharacters safely.
// Cancelling ctx terminates the command (see Options.Group for what
// exactly is signalled); the caller distinguishes that from a genuine
// failure by checking ctx itself.
// It returns the command's exit code; err is non-nil only when the
// command could not be started at all.
func RunWith(ctx context.Context, c command.Command, workdir string, args []string, opts Options) (int, error) {
	if err := c.Runnable(); err != nil {
		return -1, err
	}
	cmd := exec.CommandContext(ctx, c.RunPath(), args...)
	cmd.Dir = workdir
	cmd.Stdin = cmp.Or(opts.Stdin, io.Reader(os.Stdin))
	cmd.Stdout = cmp.Or(opts.Stdout, io.Writer(os.Stdout))
	cmd.Stderr = cmp.Or(opts.Stderr, io.Writer(os.Stderr))
	if opts.Group {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		// The child leads its own group, so its pid doubles as the group
		// id to signal.
		cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
		cmd.WaitDelay = stopGrace
	}
	if err := cmd.Start(); err != nil {
		return -1, err
	}
	pgid := cmd.Process.Pid
	waitErr := cmd.Wait()
	if opts.Group && ctx.Err() != nil {
		// Whatever outlived the SIGTERM and the leader's own kill is swept
		// up here. A group id stays reserved while the group still has
		// members, so this can only ever reach this job's descendants.
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
	if waitErr != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](waitErr); ok {
			return exitErr.ExitCode(), nil
		}
		return -1, waitErr
	}
	return 0, nil
}
