package daemon

import (
	"os"
	"path/filepath"
)

const (
	// jobsSubdir holds one directory per job under the state directory.
	jobsSubdir = "jobs"
	// RecordFile is the persisted JobInfo inside a job directory.
	RecordFile = "job.json"
	// LogFile is the combined stdout/stderr of a job.
	LogFile = "output.log"
)

// StateDir returns the directory exqd keeps job state in:
// $XDG_STATE_HOME/exq, or ~/.local/state/exq when XDG_STATE_HOME is
// unset. Both exq and exqd derive it the same way, which is what lets
// `exq logs` read a log file exqd wrote without asking the daemon.
func StateDir() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "exq")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// Must stay absolute: exq runs from varying directories, and a
		// relative state path would point somewhere different every time.
		return filepath.Join(os.TempDir(), "exq-state")
	}
	return filepath.Join(home, ".local", "state", "exq")
}

// JobsDir returns the directory holding one subdirectory per job.
func JobsDir() string {
	return filepath.Join(StateDir(), jobsSubdir)
}

// JobDir returns the directory of a single job.
func JobDir(id string) string {
	return filepath.Join(JobsDir(), id)
}

// JobLogPath returns the log file of a single job.
func JobLogPath(id string) string {
	return filepath.Join(JobDir(id), LogFile)
}
