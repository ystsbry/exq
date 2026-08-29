package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ystsbry/exq/internal/daemon"
	"github.com/ystsbry/exq/internal/daemon/server"
)

// startDaemon runs a real exqd server and points both the socket and the
// state directory at it for the duration of the test, so the commands
// under test reach it the way they would in normal use.
func startDaemon(t *testing.T) {
	t.Helper()
	// Unix socket paths are capped around 108 bytes and t.TempDir can
	// exceed that, so the runtime dir is created directly under /tmp.
	runtimeDir, err := os.MkdirTemp("", "exq")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	jobs, err := server.NewJobs(daemon.JobsDir())
	if err != nil {
		t.Fatal(err)
	}
	ln, err := server.Listen(daemon.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.New(jobs, ln, nil).Serve(t.Context())
	}()
	t.Cleanup(func() {
		<-done
		jobs.StopAll()
	})
}

// waitJob polls a job record until it reaches a terminal state.
func waitJob(t *testing.T, id string) daemon.JobInfo {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		info, err := daemon.ReadJobRecord(id)
		if err == nil && info.State.Done() {
			return *info
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s never finished", id)
	return daemon.JobInfo{}
}

// submittedID pulls the job id out of what `exq run --bg` printed.
func submittedID(t *testing.T, out string) string {
	t.Helper()
	_, rest, ok := strings.Cut(out, "as job ")
	if !ok {
		t.Fatalf("no job id in %q", out)
	}
	id, _, _ := strings.Cut(rest, "\n")
	return strings.TrimSpace(id)
}

func TestRunBackgroundSubmitsAndJobsListsIt(t *testing.T) {
	startDaemon(t)
	st := newStore(t)
	addScript(t, st, "greet", "description = \"hi\"\n", "#!/bin/sh\necho \"hello $1\"\n")

	out, err := run(t, "", "run", "greet", "--bg", "--", "world")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "submitted greet as job ") {
		t.Fatalf("run --bg output = %q", out)
	}
	id := submittedID(t, out)
	done := waitJob(t, id)
	if done.State != daemon.JobSucceeded {
		t.Fatalf("job = %q reason %q, want succeeded", done.State, done.Reason)
	}
	if done.Spec.Workdir != st.Root {
		t.Fatalf("workdir = %q, want the directory exq ran in (%q)", done.Spec.Workdir, st.Root)
	}

	listed, err := run(t, "", "jobs")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed, id) || !strings.Contains(listed, "succeeded") || !strings.Contains(listed, "greet") {
		t.Fatalf("jobs output = %q", listed)
	}

	logs, err := run(t, "", "logs", id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs, "hello world") {
		t.Fatalf("logs output = %q", logs)
	}
}

func TestRunBackgroundPassesTheScheduleID(t *testing.T) {
	startDaemon(t)
	st := newStore(t)
	addScript(t, st, "noop", "", "#!/bin/sh\n")

	out, err := run(t, "", "run", "noop", "--bg", "--schedule-id", "exq-nightly")
	if err != nil {
		t.Fatal(err)
	}
	done := waitJob(t, submittedID(t, out))
	if done.Spec.ScheduleID != "exq-nightly" {
		t.Fatalf("schedule id = %q, want exq-nightly", done.Spec.ScheduleID)
	}
}

func TestRunBackgroundRejectsAnUnknownCommandUpFront(t *testing.T) {
	startDaemon(t)
	newStore(t)

	if _, err := run(t, "", "run", "ghost", "--bg"); err == nil {
		t.Fatal("want an error for an unknown command")
	}
	// Nothing should have reached the daemon: a typo must not leave a
	// failed job record behind.
	jobs, err := daemonClient().List()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs = %+v, want none", jobs)
	}
}

func TestJobsReportsAnEmptyList(t *testing.T) {
	startDaemon(t)
	newStore(t)

	out, err := run(t, "", "jobs")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No jobs yet") {
		t.Fatalf("jobs output = %q", out)
	}
}

func TestStopEndsARunningJob(t *testing.T) {
	startDaemon(t)
	st := newStore(t)
	addScript(t, st, "sleeper", "", "#!/bin/sh\necho up\nsleep 60\n")

	out, err := run(t, "", "run", "sleeper", "--bg")
	if err != nil {
		t.Fatal(err)
	}
	id := submittedID(t, out)
	// Wait for the process to be up so the stop hits a running job.
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, err := os.ReadFile(daemon.JobLogPath(id))
		if err == nil && strings.Contains(string(data), "up") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job never started")
		}
		time.Sleep(10 * time.Millisecond)
	}

	stopped, err := run(t, "", "stop", id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stopped, "stopped") {
		t.Fatalf("stop output = %q", stopped)
	}
	if got := waitJob(t, id); got.State != daemon.JobStopped {
		t.Fatalf("state = %q, want stopped", got.State)
	}
}

func TestLogsFollowsUntilTheJobFinishes(t *testing.T) {
	startDaemon(t)
	st := newStore(t)
	addScript(t, st, "ticker", "", "#!/bin/sh\nfor i in 1 2 3; do echo tick-$i; sleep 0.1; done\n")

	out, err := run(t, "", "run", "ticker", "--bg")
	if err != nil {
		t.Fatal(err)
	}
	id := submittedID(t, out)

	followed, err := run(t, "", "logs", id, "-f")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"tick-1", "tick-2", "tick-3"} {
		if !strings.Contains(followed, want) {
			t.Fatalf("followed log = %q, missing %q", followed, want)
		}
	}
}

func TestLogsAndStopRejectUnknownJobs(t *testing.T) {
	startDaemon(t)
	newStore(t)

	if _, err := run(t, "", "logs", "20260829-010203-dead"); err == nil {
		t.Fatal("logs of an unknown job: want an error")
	}
	if _, err := run(t, "", "stop", "20260829-010203-dead"); err == nil {
		t.Fatal("stop of an unknown job: want an error")
	}
}

func TestLogsReportsAJobThatProducedNoOutput(t *testing.T) {
	startDaemon(t)
	newStore(t)
	// A record without a log file is what a job that never opened one
	// leaves behind.
	id := "20260829-010203-beef"
	if err := os.MkdirAll(daemon.JobDir(id), 0o700); err != nil {
		t.Fatal(err)
	}
	record := `{"id":"` + id + `","state":"succeeded","spec":{"name":"noop"}}`
	if err := os.WriteFile(filepath.Join(daemon.JobDir(id), daemon.RecordFile), []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, "", "logs", id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "(no output)") {
		t.Fatalf("logs output = %q", out)
	}
}

func TestJobCommandsExplainAMissingDaemon(t *testing.T) {
	// Point at a socket that is not there: no daemon was started.
	dir, err := os.MkdirTemp("", "exq")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	st := newStore(t)
	addScript(t, st, "noop", "", "#!/bin/sh\n")

	for _, args := range [][]string{{"jobs"}, {"run", "noop", "--bg"}, {"stop", "x"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, err := run(t, "", args...)
			if err == nil {
				t.Fatal("want an error")
			}
			if !errors.Is(err, daemon.ErrUnreachable) {
				t.Fatalf("err = %v, want it to wrap ErrUnreachable", err)
			}
			if !strings.Contains(err.Error(), "exq daemon install") {
				t.Fatalf("err = %v, want it to point at `exq daemon install`", err)
			}
		})
	}
}

func TestRunBackgroundReportsASkippedScheduleRun(t *testing.T) {
	startDaemon(t)
	st := newStore(t)
	addScript(t, st, "slow", "", "#!/bin/sh\nsleep 60\n")

	first, err := run(t, "", "run", "slow", "--bg", "--schedule-id", "proj-slow")
	if err != nil {
		t.Fatal(err)
	}
	id := submittedID(t, first)
	deadline := time.Now().Add(10 * time.Second)
	for {
		info, err := daemon.ReadJobRecord(id)
		if err == nil && info.State == daemon.JobRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job never started")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The timer's oneshot service must exit cleanly on a skip, or every
	// overlapping firing would show up as a failed unit.
	out, err := run(t, "", "run", "slow", "--bg", "--schedule-id", "proj-slow")
	if err != nil {
		t.Fatalf("a skipped submit must not fail: %v", err)
	}
	if !strings.Contains(out, "not started") || !strings.Contains(out, id) {
		t.Fatalf("skip output = %q, want it to name the job still running", out)
	}
	if _, err := run(t, "", "stop", id); err != nil {
		t.Fatal(err)
	}
}
