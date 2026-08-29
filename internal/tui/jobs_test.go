package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ystsbry/exq/internal/command"
	"github.com/ystsbry/exq/internal/daemon"
	"github.com/ystsbry/exq/internal/store"
)

// fakeJobs stands in for the exqd client: it records what was submitted
// and answers from a fixed list.
type fakeJobs struct {
	submitted []daemon.JobSpec
	reply     *daemon.JobInfo
	submitErr error
	list      []daemon.JobInfo
	listErr   error
}

func (f *fakeJobs) Submit(spec daemon.JobSpec) (*daemon.JobInfo, error) {
	f.submitted = append(f.submitted, spec)
	if f.submitErr != nil {
		return nil, f.submitErr
	}
	info := f.reply
	if info == nil {
		info = &daemon.JobInfo{ID: "20260829-010203-a1b2", Spec: spec, State: daemon.JobRunning}
	}
	f.list = append([]daemon.JobInfo{*info}, f.list...)
	return info, nil
}

func (f *fakeJobs) List() ([]daemon.JobInfo, error) {
	return f.list, f.listErr
}

// jobsModel is a browser wired to a fake daemon, with the job list
// already loaded.
func jobsModel(t *testing.T, items []command.Command, jobs *fakeJobs) model {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(st, items)
	m.deps = Deps{Jobs: jobs}
	return m.refreshJobs()
}

func TestBackgroundKeySubmitsAndShowsTheJob(t *testing.T) {
	jobs := &fakeJobs{}
	m := jobsModel(t, []command.Command{{Name: "build", Kind: command.KindScript}}, jobs)

	out := step(t, m, key("b"))
	if len(jobs.submitted) != 1 {
		t.Fatalf("submitted %d jobs, want 1", len(jobs.submitted))
	}
	spec := jobs.submitted[0]
	if spec.Name != "build" || spec.ProjectDir != m.store.Root || spec.Workdir != m.store.Root {
		t.Fatalf("spec = %+v, want the browsed command in the store's directory", spec)
	}
	// Submitting switches to the jobs tab, which is where the result is.
	if out.active != out.jobsTab() {
		t.Fatalf("active tab = %d, want the jobs tab %d", out.active, out.jobsTab())
	}
	view := out.View()
	if !strings.Contains(view, "submitted build as job 20260829-010203-a1b2") {
		t.Fatalf("view does not report the submit:\n%s", view)
	}
	if !strings.Contains(view, "20260829-010203-a1b2") || !strings.Contains(view, "running") {
		t.Fatalf("jobs tab does not list the new job:\n%s", view)
	}
}

func TestBackgroundKeyPassesFormValues(t *testing.T) {
	jobs := &fakeJobs{}
	items := []command.Command{{
		Name: "deploy", Kind: command.KindScript,
		Args: []command.Arg{{Key: "env"}, {Key: "service"}},
	}}
	m := jobsModel(t, items, jobs)

	// enter opens the form; ctrl+b submits what has been typed rather
	// than running it here.
	out := step(t, m, key("enter"), key("p"), key("r"), key("o"), key("d"), key("tab"), key("a"), key("ctrl+b"))
	if len(jobs.submitted) != 1 {
		t.Fatalf("submitted %d jobs, want 1", len(jobs.submitted))
	}
	if got := jobs.submitted[0].Args; len(got) != 2 || got[0] != "prod" || got[1] != "a" {
		t.Fatalf("args = %v, want [prod a]", got)
	}
	if out.chosen != -1 {
		t.Fatal("a background submit must not also pick the command for the foreground")
	}
}

func TestBackgroundSubmitReportsAnUnreachableDaemon(t *testing.T) {
	jobs := &fakeJobs{submitErr: daemon.ErrUnreachable}
	m := jobsModel(t, []command.Command{{Name: "build", Kind: command.KindScript}}, jobs)

	out := step(t, m, key("b"))
	if !strings.Contains(out.View(), "exq daemon install") {
		t.Fatalf("view does not point at the fix:\n%s", out.View())
	}
}

func TestBackgroundSubmitReportsASkippedScheduleRun(t *testing.T) {
	jobs := &fakeJobs{reply: &daemon.JobInfo{
		ID: "20260829-010203-a1b2", State: daemon.JobSkipped,
		Reason: "skipped: schedule demo-build is still running as job 20260829-010000-0001",
	}}
	m := jobsModel(t, []command.Command{{Name: "build", Kind: command.KindScript}}, jobs)

	if view := step(t, m, key("b")).View(); !strings.Contains(view, "not started") {
		t.Fatalf("view does not report the skip:\n%s", view)
	}
}

func TestJobsTabReportsAnUnavailableDaemon(t *testing.T) {
	jobs := &fakeJobs{listErr: errors.New("exqd is not reachable")}
	m := jobsModel(t, nil, jobs)
	m.active = m.jobsTab()

	view := m.View()
	if !strings.Contains(view, "exqd is not reachable") || !strings.Contains(view, "exq daemon install") {
		t.Fatalf("jobs tab does not explain the missing daemon:\n%s", view)
	}
}

func TestJobsTabIsEmptyBeforeAnythingRuns(t *testing.T) {
	m := jobsModel(t, nil, &fakeJobs{})
	m.active = m.jobsTab()
	if view := m.View(); !strings.Contains(view, "no background jobs yet") {
		t.Fatalf("jobs tab missing the empty hint:\n%s", view)
	}
}

func TestJobsTabRefreshPicksUpNewJobs(t *testing.T) {
	jobs := &fakeJobs{}
	m := jobsModel(t, nil, jobs)
	m.active = m.jobsTab()

	jobs.list = []daemon.JobInfo{{
		ID: "20260829-010203-a1b2", State: daemon.JobSucceeded,
		Spec: daemon.JobSpec{Name: "build"}, StartedAt: time.Now(), FinishedAt: time.Now(),
	}}
	out := step(t, m, key("r"))
	if !strings.Contains(out.View(), "20260829-010203-a1b2") {
		t.Fatalf("refresh did not pick up the new job:\n%s", out.View())
	}
}

func TestEnterOnAJobShowsItsLog(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)
	id := "20260829-010203-a1b2"
	if err := os.MkdirAll(daemon.JobDir(id), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(daemon.JobLogPath(id), []byte("hello from the job\nsecond line\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	jobs := &fakeJobs{list: []daemon.JobInfo{{
		ID: id, State: daemon.JobSucceeded, Spec: daemon.JobSpec{Name: "build"},
	}}}
	m := jobsModel(t, nil, jobs)
	m.active = m.jobsTab()

	out := step(t, m, key("enter"))
	view := out.View()
	if !strings.Contains(view, "log: "+id) || !strings.Contains(view, "hello from the job") {
		t.Fatalf("log view = %q", view)
	}
	// esc returns to the list rather than quitting.
	back := step(t, out, key("esc"))
	if back.mode != modeBrowse || back.chosen != -1 {
		t.Fatalf("esc from the log left mode=%v chosen=%d", back.mode, back.chosen)
	}
}

func TestJobLogHandlesAJobWithoutOutput(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	jobs := &fakeJobs{list: []daemon.JobInfo{{
		ID: "20260829-010203-a1b2", State: daemon.JobQueued, Spec: daemon.JobSpec{Name: "build"},
	}}}
	m := jobsModel(t, nil, jobs)
	m.active = m.jobsTab()

	if view := step(t, m, key("enter")).View(); !strings.Contains(view, "(no output)") {
		t.Fatalf("log view = %q", view)
	}
}

func TestBackgroundKeyWithoutADaemonExplainsItself(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A browser with no Deps at all still works; only the job features
	// are unavailable.
	m := newModel(st, []command.Command{{Name: "build", Kind: command.KindScript}})
	if view := step(t, m, key("b")).View(); !strings.Contains(view, "exq daemon install") {
		t.Fatalf("view = %q", view)
	}
}
