package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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

// ReadJobRecord loads the persisted record of one job straight from the
// state directory. Job history survives the daemon, so reading it does
// not need exqd to be running — which is exactly the situation in which
// someone wants to know how the last job ended.
func ReadJobRecord(id string) (*JobInfo, error) {
	if id == "" || filepath.Base(id) != id {
		return nil, fmt.Errorf("invalid job id %q", id)
	}
	data, err := os.ReadFile(filepath.Join(JobDir(id), RecordFile))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("job %q not found under %s", id, JobsDir())
		}
		return nil, err
	}
	var info JobInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("job %q: malformed record: %w", id, err)
	}
	return &info, nil
}
