package daemon

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// PruneResult reports what a prune removed and what it left alone.
type PruneResult struct {
	Removed int   // job directories deleted
	Freed   int64 // bytes those directories occupied
	Kept    int   // jobs left because they are still running
}

// PruneJobs deletes the directories of jobs that have finished —
// succeeded, failed, stopped or skipped — and reports what it freed.
// Jobs that are still queued or running are left alone, records included.
//
// Pruning goes straight at the state directory rather than through the
// daemon: job history is files, a finished job's record is never written
// again, and history is exactly what one wants to clean up when exqd is
// not even running.
func PruneJobs() (*PruneResult, error) {
	entries, err := os.ReadDir(JobsDir())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &PruneResult{}, nil
		}
		return nil, err
	}
	res := &PruneResult{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := ReadJobRecord(e.Name())
		if err != nil {
			// A directory without a readable record is not a job worth
			// keeping, but it is also not ours to guess about: leave it.
			continue
		}
		if !info.State.Done() {
			res.Kept++
			continue
		}
		dir := JobDir(e.Name())
		size := dirSize(dir)
		if err := os.RemoveAll(dir); err != nil {
			return nil, fmt.Errorf("remove %s: %w", dir, err)
		}
		res.Removed++
		res.Freed += size
	}
	return res, nil
}

// dirSize is the total size of the files in a job directory. A file that
// cannot be measured simply counts as zero: the number is there to give
// the user a sense of scale, not to be exact.
func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
