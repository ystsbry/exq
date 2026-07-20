// Package runner executes exq commands, wiring the entrypoint to the
// user's terminal and reporting the exit code.
package runner

import (
	"errors"
	"os"
	"os/exec"

	"github.com/ystsbry/exq/internal/command"
)

// Run executes c's entrypoint with the working directory set to workdir
// (the directory the user invoked exq from, not the command directory).
// args are passed through verbatim as $1, $2, … — no shell is involved, so
// values may contain spaces or metacharacters safely.
// It returns the command's exit code; err is non-nil only when the command
// could not be started at all.
func Run(c command.Command, workdir string, args []string) (int, error) {
	if err := c.Runnable(); err != nil {
		return -1, err
	}
	cmd := exec.Command(c.RunPath(), args...)
	cmd.Dir = workdir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
}
