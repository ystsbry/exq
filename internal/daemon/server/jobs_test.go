package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ystsbry/exq/internal/command"
	"github.com/ystsbry/exq/internal/daemon"
	"github.com/ystsbry/exq/internal/store"
)

// newProject creates a store with one script per entry of scripts, keyed
// by name with the run.sh body as value, and returns its root.
func newProject(t *testing.T, scripts map[string]string) string {
	t.Helper()
	root := t.TempDir()
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(st.ScriptsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range scripts {
		dir := filepath.Join(st.ScriptsDir(), name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, command.RunFile), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// newJobs returns an engine writing into a fresh temporary directory.
func newJobs(t *testing.T) *Jobs {
	t.Helper()
	j, err := NewJobs(filepath.Join(t.TempDir(), "jobs"))
	if err != nil {
		t.Fatal(err)
	}
	return j
}

// waitState polls until the job reaches a terminal state, failing the
// test if it never settles.
func waitState(t *testing.T, j *Jobs, id string) daemon.JobInfo {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		info, err := j.Status(id)
		if err != nil {
			t.Fatal(err)
		}
		if info.State.Done() {
			return *info
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s never reached a terminal state", id)
	return daemon.JobInfo{}
}

func (j *Jobs) logOf(t *testing.T, id string) string {
	t.Helper()
	data, err := os.ReadFile(j.logPath(id))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSubmitRunsScriptInWorkdir(t *testing.T) {
	root := newProject(t, map[string]string{
		"greet": "#!/bin/sh\necho \"hello $1\"\npwd\n",
	})
	work := t.TempDir()
	j := newJobs(t)

	info, err := j.Submit(daemon.JobSpec{
		ProjectDir: root, Workdir: work, Name: "greet", Args: []string{"world"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if info.State != daemon.JobQueued {
		t.Fatalf("state right after submit = %q, want queued", info.State)
	}

	done := waitState(t, j, info.ID)
	if done.State != daemon.JobSucceeded || done.ExitCode != 0 {
		t.Fatalf("job = %q exit %d, want succeeded exit 0", done.State, done.ExitCode)
	}
	if done.StartedAt.IsZero() || done.FinishedAt.IsZero() {
		t.Fatalf("timestamps not recorded: %+v", done)
	}
	log := j.logOf(t, info.ID)
	if !strings.Contains(log, "hello world") {
		t.Fatalf("log missing the script output: %q", log)
	}
	// The pwd line proves the child ran in workdir, not in the project or
	// the daemon's own directory.
	resolved, err := filepath.EvalSymlinks(work)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log, resolved) {
		t.Fatalf("log %q does not show the working directory %s", log, resolved)
	}
}

func TestSubmitRecordsFailingExitCode(t *testing.T) {
	root := newProject(t, map[string]string{
		"boom": "#!/bin/sh\necho nope >&2\nexit 3\n",
	})
	j := newJobs(t)

	info, err := j.Submit(daemon.JobSpec{ProjectDir: root, Workdir: root, Name: "boom"})
	if err != nil {
		t.Fatal(err)
	}
	done := waitState(t, j, info.ID)
	if done.State != daemon.JobFailed || done.ExitCode != 3 {
		t.Fatalf("job = %q exit %d, want failed exit 3", done.State, done.ExitCode)
	}
	if log := j.logOf(t, info.ID); !strings.Contains(log, "nope") {
		t.Fatalf("stderr missing from the log: %q", log)
	}
}

func TestSubmitFailsOnMissingWorkdir(t *testing.T) {
	root := newProject(t, map[string]string{"noop": "#!/bin/sh\n"})
	gone := filepath.Join(t.TempDir(), "removed")
	j := newJobs(t)

	info, err := j.Submit(daemon.JobSpec{ProjectDir: root, Workdir: gone, Name: "noop"})
	if err != nil {
		t.Fatal(err)
	}
	done := waitState(t, j, info.ID)
	if done.State != daemon.JobFailed {
		t.Fatalf("job = %q, want failed", done.State)
	}
	if !strings.Contains(done.Reason, gone) {
		t.Fatalf("reason %q does not name the missing workdir", done.Reason)
	}
	// The reason has to be visible from `exq logs` too, not just in the
	// record: the log is where a user looks first.
	if log := j.logOf(t, info.ID); !strings.Contains(log, gone) {
		t.Fatalf("log %q does not explain the failure", log)
	}
}

func TestSubmitFailsOnUnknownCommand(t *testing.T) {
	root := newProject(t, nil)
	j := newJobs(t)

	info, err := j.Submit(daemon.JobSpec{ProjectDir: root, Workdir: root, Name: "ghost"})
	if err != nil {
		t.Fatal(err)
	}
	done := waitState(t, j, info.ID)
	if done.State != daemon.JobFailed || !strings.Contains(done.Reason, "ghost") {
		t.Fatalf("job = %q reason %q, want failed naming the command", done.State, done.Reason)
	}
}

func TestSubmitResolvesTheCommandAtRunTime(t *testing.T) {
	root := newProject(t, map[string]string{"greet": "#!/bin/sh\necho original\n"})
	j := newJobs(t)
	// Rewrite the entrypoint between building the spec and submitting it:
	// what runs must be what is on disk now, not a snapshot.
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(st.ScriptsDir(), "greet", command.RunFile)
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho updated\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	info, err := j.Submit(daemon.JobSpec{ProjectDir: root, Workdir: root, Name: "greet"})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, j, info.ID)
	if log := j.logOf(t, info.ID); !strings.Contains(log, "updated") {
		t.Fatalf("log %q shows the stale script", log)
	}
}

func TestSubmitRejectsIncompleteSpec(t *testing.T) {
	j := newJobs(t)
	specs := map[string]daemon.JobSpec{
		"no name":        {ProjectDir: "/tmp", Workdir: "/tmp"},
		"no project dir": {Workdir: "/tmp", Name: "x"},
		"no workdir":     {ProjectDir: "/tmp", Name: "x"},
		"relative dir":   {ProjectDir: "rel", Workdir: "/tmp", Name: "x"},
	}
	for label, spec := range specs {
		t.Run(label, func(t *testing.T) {
			if _, err := j.Submit(spec); err == nil {
				t.Fatal("want an error, got nil")
			}
		})
	}
}

func TestStopTerminatesTheProcessGroup(t *testing.T) {
	root := newProject(t, map[string]string{
		// The child sleep inherits the group, so a stop that only killed
		// the direct child would leave it behind.
		"sleeper": "#!/bin/sh\nsleep 60 &\necho started\nwait\n",
	})
	j := newJobs(t)

	info, err := j.Submit(daemon.JobSpec{ProjectDir: root, Workdir: root, Name: "sleeper"})
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the script to actually be up before stopping it.
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(readFileOrEmpty(j.logPath(info.ID)), "started") {
		if time.Now().After(deadline) {
			t.Fatal("script never started")
		}
		time.Sleep(10 * time.Millisecond)
	}

	stopped, err := j.Stop(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.State != daemon.JobStopped {
		t.Fatalf("state after stop = %q, want stopped", stopped.State)
	}
	if done := waitState(t, j, info.ID); done.State != daemon.JobStopped {
		t.Fatalf("final state = %q, want stopped", done.State)
	}
}

func TestStopOnFinishedJobIsANoop(t *testing.T) {
	root := newProject(t, map[string]string{"noop": "#!/bin/sh\n"})
	j := newJobs(t)
	info, err := j.Submit(daemon.JobSpec{ProjectDir: root, Workdir: root, Name: "noop"})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, j, info.ID)

	got, err := j.Stop(info.ID)
	if err != nil {
		t.Fatalf("stopping a finished job: %v", err)
	}
	if got.State != daemon.JobSucceeded {
		t.Fatalf("state = %q, want the untouched succeeded", got.State)
	}
}

func TestStatusAndStopRejectUnknownIDs(t *testing.T) {
	j := newJobs(t)
	if _, err := j.Status("nope"); err == nil {
		t.Fatal("status of an unknown job: want an error")
	}
	if _, err := j.Stop("../escape"); err == nil {
		t.Fatal("stop with a traversing id: want an error")
	}
}

func TestListReturnsNewestFirst(t *testing.T) {
	root := newProject(t, map[string]string{"noop": "#!/bin/sh\n"})
	j := newJobs(t)
	base := time.Date(2026, 8, 29, 1, 2, 3, 0, time.UTC)
	var ids []string
	for i := range 3 {
		j.now = func() time.Time { return base.Add(time.Duration(i) * time.Minute) }
		info, err := j.Submit(daemon.JobSpec{ProjectDir: root, Workdir: root, Name: "noop"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, info.ID)
	}
	j.Wait()

	jobs, err := j.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 3 {
		t.Fatalf("listed %d jobs, want 3", len(jobs))
	}
	for i, want := range []string{ids[2], ids[1], ids[0]} {
		if jobs[i].ID != want {
			t.Fatalf("jobs[%d] = %s, want %s", i, jobs[i].ID, want)
		}
	}
}

func TestListSkipsUnreadableRecords(t *testing.T) {
	j := newJobs(t)
	if err := os.MkdirAll(filepath.Join(j.dir, "20260829-010203-beef"), 0o700); err != nil {
		t.Fatal(err)
	}
	jobs, err := j.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("listed %d jobs, want none", len(jobs))
	}
}

func TestRecoverFailsJobsLeftRunning(t *testing.T) {
	j := newJobs(t)
	stale := &daemon.JobInfo{
		ID:        "20260829-010203-dead",
		Spec:      daemon.JobSpec{ProjectDir: "/tmp", Workdir: "/tmp", Name: "noop"},
		State:     daemon.JobRunning,
		CreatedAt: time.Now(),
	}
	if err := os.MkdirAll(filepath.Join(j.dir, stale.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := j.save(stale); err != nil {
		t.Fatal(err)
	}

	n, err := j.Recover()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("recovered %d jobs, want 1", n)
	}
	got, err := j.Status(stale.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != daemon.JobFailed || !strings.Contains(got.Reason, "restart") {
		t.Fatalf("recovered job = %q reason %q, want failed with a restart reason", got.State, got.Reason)
	}
}

func TestRecoverLeavesFinishedJobsAlone(t *testing.T) {
	root := newProject(t, map[string]string{"noop": "#!/bin/sh\n"})
	j := newJobs(t)
	info, err := j.Submit(daemon.JobSpec{ProjectDir: root, Workdir: root, Name: "noop"})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, j, info.ID)

	n, err := j.Recover()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("recovered %d jobs, want none", n)
	}
	got, _ := j.Status(info.ID)
	if got.State != daemon.JobSucceeded {
		t.Fatalf("state = %q, want the untouched succeeded", got.State)
	}
}

func TestJobIDsSortChronologically(t *testing.T) {
	base := time.Date(2026, 8, 29, 1, 2, 3, 0, time.UTC)
	first := newID(base)
	second := newID(base.Add(time.Second))
	if first >= second {
		t.Fatalf("ids do not sort by time: %s !< %s", first, second)
	}
	// Two ids minted for the same instant still have to differ, or a
	// burst of submits would overwrite each other's records.
	seen := map[string]bool{}
	for range 16 {
		id := newID(base)
		if seen[id] {
			t.Fatalf("id %s minted twice for the same instant", id)
		}
		seen[id] = true
	}
}

// readFileOrEmpty reads path, treating any failure as "nothing yet".
func readFileOrEmpty(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// newWorkflow adds a workflow directory (command.toml only) to a project
// created by newProject.
func newWorkflow(t *testing.T, root, name, meta string) {
	t.Helper()
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(st.WorkflowsDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, command.MetaFile), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitRunsWorkflowIntoTheJobLog(t *testing.T) {
	root := newProject(t, map[string]string{
		"one": "#!/bin/sh\necho step-one\n",
		"two": "#!/bin/sh\necho step-two\n",
	})
	newWorkflow(t, root, "both", "steps = [\"one\", \"two\"]\n")
	j := newJobs(t)

	info, err := j.Submit(daemon.JobSpec{ProjectDir: root, Workdir: root, Name: "both"})
	if err != nil {
		t.Fatal(err)
	}
	done := waitState(t, j, info.ID)
	if done.State != daemon.JobSucceeded {
		t.Fatalf("job = %q reason %q, want succeeded", done.State, done.Reason)
	}
	log := j.logOf(t, info.ID)
	for _, want := range []string{"[1/2] one", "step-one", "[2/2] two", "step-two", "all 2 steps succeeded"} {
		if !strings.Contains(log, want) {
			t.Fatalf("log %q is missing %q", log, want)
		}
	}
}

func TestWorkflowJobTakesTheFailingStepsExitCode(t *testing.T) {
	root := newProject(t, map[string]string{
		"ok":   "#!/bin/sh\n",
		"bad":  "#!/bin/sh\nexit 4\n",
		"last": "#!/bin/sh\necho never\n",
	})
	newWorkflow(t, root, "flow", "steps = [\"ok\", \"bad\", \"last\"]\n")
	j := newJobs(t)

	info, err := j.Submit(daemon.JobSpec{ProjectDir: root, Workdir: root, Name: "flow"})
	if err != nil {
		t.Fatal(err)
	}
	done := waitState(t, j, info.ID)
	if done.State != daemon.JobFailed || done.ExitCode != 4 {
		t.Fatalf("job = %q exit %d, want failed exit 4", done.State, done.ExitCode)
	}
	log := j.logOf(t, info.ID)
	if !strings.Contains(log, "failed at step bad") {
		t.Fatalf("log %q does not name the failing step", log)
	}
	if !strings.Contains(log, "- last") {
		t.Fatalf("log %q does not show the skipped step in the summary", log)
	}
	if strings.Contains(log, "never") {
		t.Fatalf("log %q shows output of a step that should have been skipped", log)
	}
}

func TestWorkflowJobFailsPreFlightWithoutRunningAnything(t *testing.T) {
	root := newProject(t, nil)
	newWorkflow(t, root, "flow", "steps = [\"ghost\"]\n")
	j := newJobs(t)

	info, err := j.Submit(daemon.JobSpec{ProjectDir: root, Workdir: root, Name: "flow"})
	if err != nil {
		t.Fatal(err)
	}
	done := waitState(t, j, info.ID)
	if done.State != daemon.JobFailed || !strings.Contains(done.Reason, "ghost") {
		t.Fatalf("job = %q reason %q, want failed naming the unknown step", done.State, done.Reason)
	}
}

func TestStoppingAWorkflowJobSkipsTheRemainingSteps(t *testing.T) {
	root := newProject(t, map[string]string{
		"slow": "#!/bin/sh\necho running\nsleep 60\n",
		"next": "#!/bin/sh\necho next-ran\n",
	})
	newWorkflow(t, root, "flow", "steps = [\"slow\", \"next\"]\n")
	j := newJobs(t)

	info, err := j.Submit(daemon.JobSpec{ProjectDir: root, Workdir: root, Name: "flow"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(readFileOrEmpty(j.logPath(info.ID)), "running") {
		if time.Now().After(deadline) {
			t.Fatal("first step never started")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := j.Stop(info.ID); err != nil {
		t.Fatal(err)
	}
	done := waitState(t, j, info.ID)
	if done.State != daemon.JobStopped {
		t.Fatalf("job = %q, want stopped", done.State)
	}
	if log := j.logOf(t, info.ID); strings.Contains(log, "next-ran") {
		t.Fatalf("log %q shows a step started after the stop", log)
	}
}
