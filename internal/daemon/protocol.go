// Package daemon defines the wire protocol between exq and the exqd
// background-job daemon, plus the client side of it. The transport is
// deliberately plain — one JSON line request, one JSON line response,
// one request per connection over a unix socket — mirroring the
// internal/herdr reporter. Unlike herdr, though, nothing here is
// best-effort: callers act on the daemon's answer, so every failure
// surfaces as an error.
package daemon

import "time"

// ProtocolVersion is stamped on every request and response. exq and
// exqd are separate binaries that can drift apart across upgrades; a
// mismatch is reported to the caller so it can suggest restarting the
// daemon.
const ProtocolVersion = 1

// Op names one daemon operation. Schedule management is not part of
// the protocol: exq manipulates systemd timer units directly, and
// scheduled runs reach exqd through the same job.submit as manual ones.
type Op string

const (
	OpPing      Op = "ping"
	OpJobSubmit Op = "job.submit"
	OpJobList   Op = "job.list"
	OpJobStatus Op = "job.status"
	OpJobStop   Op = "job.stop"
)

// Request is the single JSON object a client sends per connection.
type Request struct {
	Version int `json:"version"`
	Op      Op  `json:"op"`
	// Job carries the spec for job.submit.
	Job *JobSpec `json:"job,omitempty"`
	// JobID addresses an existing job for job.status and job.stop.
	JobID string `json:"job_id,omitempty"`
}

// Response is the single JSON object the daemon answers with.
type Response struct {
	Version int    `json:"version"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	// Job is set for job.submit, job.status and job.stop.
	Job *JobInfo `json:"job,omitempty"`
	// Jobs is set for job.list.
	Jobs []JobInfo `json:"jobs,omitempty"`
}

// JobSpec tells exqd what to run and where. ProjectDir is the root the
// .exq/ store is resolved from — at execution time, by name, so a
// script edited between submit and run executes in its latest form.
// Workdir becomes the child process's working directory, exactly like
// the directory exq is invoked from in a synchronous run.
type JobSpec struct {
	ProjectDir string   `json:"project_dir"`
	Workdir    string   `json:"workdir"`
	Name       string   `json:"name"`
	Args       []string `json:"args,omitempty"`
	// ScheduleID marks a submit coming from a systemd schedule timer;
	// exqd uses it to skip overlapping runs of the same schedule.
	ScheduleID string `json:"schedule_id,omitempty"`
}

// JobState is the lifecycle of one job. A stop request moves a running
// job to stopped; failed covers both non-zero exits and jobs that never
// started (missing workdir, unrunnable command, daemon restart).
type JobState string

const (
	JobQueued    JobState = "queued"
	JobRunning   JobState = "running"
	JobSucceeded JobState = "succeeded"
	JobFailed    JobState = "failed"
	JobStopped   JobState = "stopped"
)

// Done reports whether the state is terminal.
func (s JobState) Done() bool {
	return s == JobSucceeded || s == JobFailed || s == JobStopped
}

// JobInfo is the daemon's record of one job, as persisted in job.json
// and returned to clients.
type JobInfo struct {
	ID       string   `json:"id"`
	Spec     JobSpec  `json:"spec"`
	State    JobState `json:"state"`
	ExitCode int      `json:"exit_code"`
	// Reason explains a failed state that has no exit code to speak of,
	// e.g. a missing workdir or an orphaned job after a daemon restart.
	Reason     string    `json:"reason,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	StartedAt  time.Time `json:"started_at,omitzero"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
}
