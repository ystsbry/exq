package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ystsbry/exq/internal/command"
	"github.com/ystsbry/exq/internal/daemon"
	"github.com/ystsbry/exq/internal/schedule"
	"github.com/ystsbry/exq/internal/store"
)

// fakeSchedules stands in for the schedule manager: it records what was
// registered and removed, and answers from a fixed list.
type fakeSchedules struct {
	added     []schedule.Spec
	addErr    error
	removed   []string
	removeErr error
	list      []schedule.Schedule
	listErr   error
	unit      string
}

func (f *fakeSchedules) Add(spec schedule.Spec) (*schedule.Schedule, error) {
	f.added = append(f.added, spec)
	if f.addErr != nil {
		return nil, f.addErr
	}
	s := schedule.Schedule{
		ID: "proj-" + spec.Name, Name: spec.Name,
		ProjectDir: spec.ProjectDir, Workdir: spec.Workdir,
		OnCalendar: spec.OnCalendar, Values: spec.Values,
	}
	f.list = append(f.list, s)
	return &s, nil
}

func (f *fakeSchedules) List() ([]schedule.Schedule, error) { return f.list, f.listErr }

func (f *fakeSchedules) Remove(id string) error {
	f.removed = append(f.removed, id)
	if f.removeErr != nil {
		return f.removeErr
	}
	kept := f.list[:0]
	for _, s := range f.list {
		if s.ID != id {
			kept = append(kept, s)
		}
	}
	f.list = kept
	return nil
}

func (f *fakeSchedules) ReadUnit(string) (string, error) { return f.unit, nil }

// schedModel is a browser wired to a fake schedule manager, with the
// schedule list already loaded.
func schedModel(t *testing.T, items []command.Command, sched *fakeSchedules) model {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(st, items)
	m.deps = Deps{Schedules: sched}
	return m.refreshSchedules()
}

func TestScheduleKeyOpensTheFormAndRegisters(t *testing.T) {
	sched := &fakeSchedules{}
	items := []command.Command{{
		Name: "deploy", Kind: command.KindScript,
		Args: []command.Arg{{Key: "env"}},
	}}
	m := schedModel(t, items, sched)

	// s opens the form with the calendar field first, then the args.
	form := step(t, m, key("s"))
	if form.mode != modeScheduleForm {
		t.Fatalf("mode = %v, want the schedule form", form.mode)
	}
	if view := form.View(); !strings.Contains(view, "on-calendar") || !strings.Contains(view, "env") {
		t.Fatalf("form = %q, want the calendar field and the declared args", view)
	}

	out := step(t, form, key("d"), key("a"), key("i"), key("l"), key("y"), key("tab"),
		key("p"), key("r"), key("o"), key("d"), key("enter"))
	if len(sched.added) != 1 {
		t.Fatalf("registered %d schedules, want 1", len(sched.added))
	}
	spec := sched.added[0]
	if spec.Name != "deploy" || spec.OnCalendar != "daily" {
		t.Fatalf("spec = %+v", spec)
	}
	if len(spec.Values) != 1 || spec.Values[0] != "prod" {
		t.Fatalf("values = %v, want [prod]", spec.Values)
	}
	// Registering lands on the schedules tab so the result is visible.
	if out.active != out.schedulesTab() {
		t.Fatalf("active tab = %d, want the schedules tab %d", out.active, out.schedulesTab())
	}
	if view := out.View(); !strings.Contains(view, "scheduled deploy as proj-deploy") {
		t.Fatalf("view does not report the registration:\n%s", view)
	}
}

func TestScheduleFormKeepsAnInvalidExpressionForEditing(t *testing.T) {
	sched := &fakeSchedules{addErr: errors.New(`invalid OnCalendar expression "nope": Failed to parse`)}
	m := schedModel(t, []command.Command{{Name: "deploy", Kind: command.KindScript}}, sched)

	out := step(t, m, key("s"), key("n"), key("o"), key("p"), key("e"), key("enter"))
	if out.mode != modeScheduleForm {
		t.Fatal("a rejected expression must leave the form open")
	}
	view := out.View()
	if !strings.Contains(view, "Failed to parse") {
		t.Fatalf("form does not show the reason:\n%s", view)
	}
	// What was typed is still there to be corrected.
	if !strings.Contains(view, "nope") {
		t.Fatalf("form lost the entered expression:\n%s", view)
	}
}

func TestScheduleFormEscReturnsWithoutRegistering(t *testing.T) {
	sched := &fakeSchedules{}
	m := schedModel(t, []command.Command{{Name: "deploy", Kind: command.KindScript}}, sched)

	out := step(t, m, key("s"), key("d"), key("esc"))
	if out.mode != modeBrowse {
		t.Fatalf("mode = %v, want browse", out.mode)
	}
	if len(sched.added) != 0 {
		t.Fatalf("registered %v despite escaping the form", sched.added)
	}
}

func TestSchedulesTabListsAndFlagsStaleEntries(t *testing.T) {
	sched := &fakeSchedules{list: []schedule.Schedule{
		{ID: "proj-nightly", Name: "nightly", OnCalendar: "daily", NextElapse: time.Now().Add(time.Hour)},
		{ID: "gone-deploy", Name: "deploy", OnCalendar: "weekly", WorkdirMissing: true},
	}}
	m := schedModel(t, nil, sched)
	m.active = m.schedulesTab()

	view := m.View()
	for _, want := range []string{"proj-nightly", "daily", "gone-deploy", "!"} {
		if !strings.Contains(view, want) {
			t.Fatalf("schedules tab = %q, missing %q", view, want)
		}
	}
}

func TestSchedulesTabReportsAnUnavailableSystemd(t *testing.T) {
	sched := &fakeSchedules{listErr: errors.New("cannot reach a systemd user instance")}
	m := schedModel(t, nil, sched)
	m.active = m.schedulesTab()

	if view := m.View(); !strings.Contains(view, "cannot reach a systemd user instance") {
		t.Fatalf("schedules tab does not explain the failure:\n%s", view)
	}
}

func TestSchedulesTabIsEmptyBeforeAnythingIsRegistered(t *testing.T) {
	m := schedModel(t, nil, &fakeSchedules{})
	m.active = m.schedulesTab()
	if view := m.View(); !strings.Contains(view, "no schedules yet") {
		t.Fatalf("schedules tab missing the empty hint:\n%s", view)
	}
}

func TestRemoveScheduleAsksFirst(t *testing.T) {
	sched := &fakeSchedules{list: []schedule.Schedule{{ID: "proj-nightly", Name: "nightly", OnCalendar: "daily"}}}
	m := schedModel(t, nil, sched)
	m.active = m.schedulesTab()

	asked := step(t, m, key("d"))
	if !strings.Contains(asked.View(), `remove schedule "proj-nightly"? [y/N]`) {
		t.Fatalf("no confirmation prompt:\n%s", asked.View())
	}
	// n keeps it, exactly like the command delete flow.
	kept := step(t, asked, key("n"))
	if len(sched.removed) != 0 {
		t.Fatalf("removed %v despite declining", sched.removed)
	}

	out := step(t, kept, key("d"), key("y"))
	if len(sched.removed) != 1 || sched.removed[0] != "proj-nightly" {
		t.Fatalf("removed = %v, want [proj-nightly]", sched.removed)
	}
	if view := out.View(); !strings.Contains(view, "no schedules yet") {
		t.Fatalf("list not refreshed after removal:\n%s", view)
	}
}

func TestRemoveScheduleReportsAFailure(t *testing.T) {
	sched := &fakeSchedules{
		list:      []schedule.Schedule{{ID: "proj-nightly", Name: "nightly"}},
		removeErr: errors.New("unit is masked"),
	}
	m := schedModel(t, nil, sched)
	m.active = m.schedulesTab()

	if view := step(t, m, key("d"), key("y")).View(); !strings.Contains(view, "unit is masked") {
		t.Fatalf("view does not report the failure:\n%s", view)
	}
}

func TestScheduleDetailShowsUnitsAndRelatedJobs(t *testing.T) {
	sched := &fakeSchedules{
		list: []schedule.Schedule{{ID: "proj-nightly", Name: "nightly", OnCalendar: "daily"}},
		unit: "[Timer]\nOnCalendar=daily\n",
	}
	m := schedModel(t, nil, sched)
	m.jobs = []daemon.JobInfo{
		{ID: "20260829-010203-a1b2", State: daemon.JobSucceeded,
			Spec: daemon.JobSpec{Name: "nightly", ScheduleID: "proj-nightly"}},
		{ID: "20260829-010203-ffff", State: daemon.JobFailed,
			Spec: daemon.JobSpec{Name: "other", ScheduleID: "proj-other"}},
	}
	m.active = m.schedulesTab()

	out := step(t, m, key("enter"))
	view := out.View()
	for _, want := range []string{"proj-nightly", "OnCalendar=daily", "20260829-010203-a1b2"} {
		if !strings.Contains(view, want) {
			t.Fatalf("detail = %q, missing %q", view, want)
		}
	}
	// Only this schedule's runs belong here.
	if strings.Contains(view, "20260829-010203-ffff") {
		t.Fatalf("detail shows a job from another schedule:\n%s", view)
	}
	if back := step(t, out, key("esc")); back.mode != modeBrowse || back.chosen != -1 {
		t.Fatalf("esc from the detail left mode=%v chosen=%d", back.mode, back.chosen)
	}
}

func TestScheduleKeyWithoutSystemdExplainsItself(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(st, []command.Command{{Name: "deploy", Kind: command.KindScript}})

	out := step(t, m, key("s"), key("enter"))
	if view := out.View(); !strings.Contains(view, "not available") {
		t.Fatalf("form does not explain the missing systemd:\n%s", view)
	}
}
