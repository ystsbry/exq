// Package server implements exqd: the unix socket server speaking the
// internal/daemon protocol, and the engine that runs submitted jobs as
// child processes of the daemon.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/ystsbry/exq/internal/daemon"
	"github.com/ystsbry/exq/internal/runner"
	"github.com/ystsbry/exq/internal/store"
)

// stopWait is how long job.stop waits for a job to actually finish
// before answering. Most entrypoints die on the SIGTERM immediately, so
// the caller usually gets the final record back; a stubborn one is
// reported as still running and settles on its own shortly after.
const stopWait = time.Second

// Jobs owns the job records under a state directory and the processes of
// the jobs that are still running. Every method is safe for concurrent
// use: the socket server handles each connection in its own goroutine.
type Jobs struct {
	dir string

	mu      sync.Mutex
	running map[string]*handle

	wg sync.WaitGroup

	// now and newID are swappable so tests get deterministic records.
	now   func() time.Time
	newID func(time.Time) string
}

// handle is the daemon's grip on one running job: the cancel that stops
// it and a channel closed once its goroutine is done.
type handle struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// NewJobs opens (creating if needed) the job directory at dir. The
// directory is private to the user: job logs can contain anything the
// entrypoint printed.
func NewJobs(dir string) (*Jobs, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	return &Jobs{
		dir:     dir,
		running: map[string]*handle{},
		now:     time.Now,
		newID:   newID,
	}, nil
}

// newID builds a job id that sorts chronologically and stays unique
// within a second: a timestamp plus four random hex digits.
func newID(t time.Time) string {
	var b [2]byte
	_, _ = rand.Read(b[:])
	return t.Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}

// Submit records a new job and starts running it. It returns as soon as
// the record exists — execution continues in the background — so the
// caller gets a job id immediately.
func (j *Jobs) Submit(spec daemon.JobSpec) (*daemon.JobInfo, error) {
	if err := validateSpec(spec); err != nil {
		return nil, err
	}
	now := j.now()
	info := &daemon.JobInfo{
		ID:        j.newID(now),
		Spec:      spec,
		State:     daemon.JobQueued,
		CreatedAt: now,
	}
	if err := os.MkdirAll(filepath.Join(j.dir, info.ID), 0o700); err != nil {
		return nil, fmt.Errorf("create job dir: %w", err)
	}
	if err := j.save(info); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	h := &handle{cancel: cancel, done: make(chan struct{})}
	j.mu.Lock()
	j.running[info.ID] = h
	j.mu.Unlock()

	j.wg.Add(1)
	go j.execute(ctx, h, *info)
	return info, nil
}

// validateSpec rejects a job that could never run, so the failure lands
// on the caller instead of in a job record nobody asked for.
func validateSpec(spec daemon.JobSpec) error {
	switch {
	case spec.Name == "":
		return errors.New("job spec has no command name")
	case spec.ProjectDir == "":
		return errors.New("job spec has no project_dir")
	case !filepath.IsAbs(spec.ProjectDir):
		return fmt.Errorf("project_dir %q must be an absolute path", spec.ProjectDir)
	case spec.Workdir == "":
		return errors.New("job spec has no workdir")
	case !filepath.IsAbs(spec.Workdir):
		return fmt.Errorf("workdir %q must be an absolute path", spec.Workdir)
	}
	return nil
}

// execute runs one job to completion and persists every state change on
// the way, so a client polling job.status sees the job progress and a
// daemon restart finds an accurate record.
func (j *Jobs) execute(ctx context.Context, h *handle, info daemon.JobInfo) {
	defer j.wg.Done()
	defer func() {
		j.mu.Lock()
		delete(j.running, info.ID)
		j.mu.Unlock()
		close(h.done)
	}()

	logFile, err := os.OpenFile(j.logPath(info.ID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		j.finish(&info, daemon.JobFailed, -1, fmt.Sprintf("open log file: %v", err))
		return
	}
	defer func() { _ = logFile.Close() }()

	info.State = daemon.JobRunning
	info.StartedAt = j.now()
	_ = j.save(&info)

	code, runErr := j.run(ctx, info.Spec, logFile)
	switch {
	case ctx.Err() != nil:
		// A stop wins over whatever the interrupted run reported — a job
		// cancelled before its process even started still comes back as
		// stopped, not as a failure the user did not cause.
		j.finish(&info, daemon.JobStopped, code, "stopped on request")
	case runErr != nil:
		// The failure never reached the entrypoint's own output, so it
		// only exists in the log if we put it there.
		fmt.Fprintf(logFile, "exq: %v\n", runErr)
		j.finish(&info, daemon.JobFailed, -1, runErr.Error())
	case code != 0:
		j.finish(&info, daemon.JobFailed, code, "")
	default:
		j.finish(&info, daemon.JobSucceeded, 0, "")
	}
}

// run resolves the job's command and executes it. Resolution happens
// here rather than at submit time so a script edited in between runs in
// its latest form — which is what a schedule registered weeks ago needs.
func (j *Jobs) run(ctx context.Context, spec daemon.JobSpec, logFile *os.File) (int, error) {
	if info, err := os.Stat(spec.Workdir); err != nil || !info.IsDir() {
		return -1, fmt.Errorf("working directory %s is not available", spec.Workdir)
	}
	st, err := store.Open(spec.ProjectDir)
	if err != nil {
		return -1, err
	}
	c, err := st.Get(spec.Name)
	if err != nil {
		return -1, err
	}
	// A background job has no terminal to read from; /dev/null makes an
	// entrypoint that reads stdin see EOF instead of hanging forever.
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return -1, err
	}
	defer func() { _ = devNull.Close() }()
	return runner.RunWith(ctx, c, spec.Workdir, spec.Args, runner.Options{
		Stdin:  devNull,
		Stdout: logFile,
		Stderr: logFile,
		Group:  true,
	})
}

// finish stamps the terminal state on info and persists it.
func (j *Jobs) finish(info *daemon.JobInfo, state daemon.JobState, code int, reason string) {
	info.State = state
	info.ExitCode = code
	info.Reason = reason
	info.FinishedAt = j.now()
	_ = j.save(info)
}

// List returns every job record, newest first.
func (j *Jobs) List() ([]daemon.JobInfo, error) {
	entries, err := os.ReadDir(j.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	jobs := make([]daemon.JobInfo, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := j.load(e.Name())
		if err != nil {
			// One unreadable record must not hide all the others.
			continue
		}
		jobs = append(jobs, *info)
	}
	slices.SortFunc(jobs, func(a, b daemon.JobInfo) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})
	return jobs, nil
}

// Status returns the current record of one job.
func (j *Jobs) Status(id string) (*daemon.JobInfo, error) {
	return j.load(id)
}

// Stop terminates a running job. It waits briefly for the job to settle
// so the answer normally carries the final record; an already finished
// job is reported as-is rather than treated as an error, which keeps
// `exq stop` idempotent.
func (j *Jobs) Stop(id string) (*daemon.JobInfo, error) {
	info, err := j.load(id)
	if err != nil {
		return nil, err
	}
	j.mu.Lock()
	h, ok := j.running[id]
	j.mu.Unlock()
	if !ok {
		return info, nil
	}
	h.cancel()
	select {
	case <-h.done:
	case <-time.After(stopWait):
	}
	return j.load(id)
}

// Recover marks jobs left in a non-terminal state as failed and returns
// how many it touched. Nothing survives a daemon restart: the children
// went down with exqd's cgroup, so a record still saying running is a
// lie from the previous process. Called once at startup, before the
// socket is served.
func (j *Jobs) Recover() (int, error) {
	jobs, err := j.List()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, info := range jobs {
		if info.State.Done() {
			continue
		}
		j.finish(&info, daemon.JobFailed, -1, "orphaned by an exqd restart")
		n++
	}
	return n, nil
}

// Wait blocks until every running job has finished. It exists for a
// graceful shutdown and for tests that need the engine to be quiet.
func (j *Jobs) Wait() {
	j.wg.Wait()
}

// StopAll cancels every running job, then waits for them.
func (j *Jobs) StopAll() {
	j.mu.Lock()
	handles := make([]*handle, 0, len(j.running))
	for _, h := range j.running {
		handles = append(handles, h)
	}
	j.mu.Unlock()
	for _, h := range handles {
		h.cancel()
	}
	j.wg.Wait()
}

func (j *Jobs) recordPath(id string) string {
	return filepath.Join(j.dir, id, daemon.RecordFile)
}

func (j *Jobs) logPath(id string) string {
	return filepath.Join(j.dir, id, daemon.LogFile)
}

// load reads one job record.
func (j *Jobs) load(id string) (*daemon.JobInfo, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(j.recordPath(id))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("job %q not found", id)
		}
		return nil, err
	}
	var info daemon.JobInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("job %q: malformed record: %w", id, err)
	}
	return &info, nil
}

// save writes a job record atomically, so a reader never catches a
// half-written state — job.json is rewritten on every transition.
func (j *Jobs) save(info *daemon.JobInfo) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	path := j.recordPath(info.ID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// validateID rejects ids that would escape the jobs directory.
func validateID(id string) error {
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id {
		return fmt.Errorf("invalid job id %q", id)
	}
	return nil
}
