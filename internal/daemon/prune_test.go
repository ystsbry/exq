package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeJob puts one job record (and a log of the given size) into the
// state directory.
func writeJob(t *testing.T, id string, state JobState, logBytes int) {
	t.Helper()
	if err := os.MkdirAll(JobDir(id), 0o700); err != nil {
		t.Fatal(err)
	}
	record, err := json.Marshal(JobInfo{ID: id, State: state, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(JobDir(id), RecordFile), record, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(JobLogPath(id), []byte(strings.Repeat("x", logBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPruneRemovesFinishedJobsOnly(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeJob(t, "20260829-010000-0001", JobSucceeded, 100)
	writeJob(t, "20260829-010001-0002", JobFailed, 200)
	writeJob(t, "20260829-010002-0003", JobStopped, 300)
	writeJob(t, "20260829-010003-0004", JobSkipped, 0)
	writeJob(t, "20260829-010004-0005", JobRunning, 50)
	writeJob(t, "20260829-010005-0006", JobQueued, 0)

	res, err := PruneJobs()
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 4 {
		t.Fatalf("removed %d jobs, want the 4 finished ones", res.Removed)
	}
	if res.Kept != 2 {
		t.Fatalf("kept %d jobs, want the 2 unfinished ones", res.Kept)
	}
	// The freed size counts the logs plus their records, so it only has
	// to be at least the log bytes.
	if res.Freed < 600 {
		t.Fatalf("freed %d bytes, want at least the 600 bytes of logs", res.Freed)
	}
	// A running job keeps everything it has: killing its record would
	// leave exqd writing into a directory that no longer exists.
	if _, err := ReadJobRecord("20260829-010004-0005"); err != nil {
		t.Fatalf("running job was pruned: %v", err)
	}
	if _, err := ReadJobRecord("20260829-010000-0001"); err == nil {
		t.Fatal("finished job survived the prune")
	}
}

func TestPruneOnAnEmptyHistoryIsANoop(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	res, err := PruneJobs()
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 0 || res.Kept != 0 {
		t.Fatalf("result = %+v, want an empty prune", res)
	}
}

func TestPruneLeavesDirectoriesItCannotRead(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	stray := JobDir("not-a-job")
	if err := os.MkdirAll(stray, 0o700); err != nil {
		t.Fatal(err)
	}

	res, err := PruneJobs()
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 0 {
		t.Fatalf("removed %d, want none", res.Removed)
	}
	// Deleting something exq does not recognize is not exq's call.
	if _, err := os.Stat(stray); err != nil {
		t.Fatalf("an unrecognized directory was removed: %v", err)
	}
}
