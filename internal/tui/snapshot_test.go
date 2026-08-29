package tui

import (
	"strings"
	"testing"

	"github.com/ystsbry/exq/internal/command"
	"github.com/ystsbry/exq/internal/store"
)

func TestSnapshotsRenderAllStates(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	items := []command.Command{
		{Name: "alpha", Description: "first sample"},
		{Name: "bravo", Description: "second sample"},
	}

	snaps := Snapshots(st, items)
	byName := map[string]string{}
	for _, s := range snaps {
		if s.View == "" {
			t.Errorf("snapshot %q rendered empty", s.Name)
		}
		byName[s.Name] = s.View
	}

	for _, want := range []string{
		"browse", "browse-workflows", "browse-jobs", "browse-jobs-unavailable",
		"job-log", "browse-schedules", "confirm-remove-schedule", "schedule-detail",
		"schedule-form", "schedule-form-invalid",
		"browse-empty", "confirm-delete", "args-form", "error",
	} {
		if _, ok := byName[want]; !ok {
			t.Errorf("missing snapshot %q", want)
		}
	}
	if !strings.Contains(byName["args-form"], "env") {
		t.Errorf("args-form snapshot should show argument keys:\n%s", byName["args-form"])
	}
	if !strings.Contains(byName["browse"], "alpha") {
		t.Errorf("browse snapshot should list commands:\n%s", byName["browse"])
	}
	if !strings.Contains(byName["browse"], "scripts (2)") {
		t.Errorf("browse snapshot should show the tab bar:\n%s", byName["browse"])
	}
	if !strings.Contains(byName["browse-workflows"], "no workflows yet") {
		t.Errorf("workflows tab snapshot should show the empty hint (fixture has no workflows):\n%s", byName["browse-workflows"])
	}
	if !strings.Contains(byName["browse-empty"], "no scripts yet") {
		t.Errorf("empty snapshot should show the empty hint:\n%s", byName["browse-empty"])
	}
	if !strings.Contains(byName["confirm-delete"], "delete") {
		t.Errorf("confirm-delete snapshot should show the prompt:\n%s", byName["confirm-delete"])
	}
	if !strings.Contains(byName["error"], "permission denied") {
		t.Errorf("error snapshot should show the error:\n%s", byName["error"])
	}
	// The jobs fixtures exist to show every state a row can be in at once.
	for _, want := range []string{"running", "succeeded", "failed", "skipped"} {
		if !strings.Contains(byName["browse-jobs"], want) {
			t.Errorf("jobs snapshot should show a %s job:\n%s", want, byName["browse-jobs"])
		}
	}
	if !strings.Contains(byName["browse-jobs-unavailable"], "exq daemon install") {
		t.Errorf("unavailable-daemon snapshot should point at the fix:\n%s", byName["browse-jobs-unavailable"])
	}
	if !strings.Contains(byName["job-log"], "all 2 steps succeeded") {
		t.Errorf("job-log snapshot should show log content:\n%s", byName["job-log"])
	}
	// The stale schedule carries the (!) marker the list uses for a
	// working directory that is gone.
	if !strings.Contains(byName["browse-schedules"], "!") {
		t.Errorf("schedules snapshot should flag the stale schedule:\n%s", byName["browse-schedules"])
	}
	if !strings.Contains(byName["schedule-form"], "on-calendar") {
		t.Errorf("schedule-form snapshot should show the calendar field:\n%s", byName["schedule-form"])
	}
	if !strings.Contains(byName["schedule-form-invalid"], "Failed to parse") {
		t.Errorf("invalid-form snapshot should show the validation error:\n%s", byName["schedule-form-invalid"])
	}
	if !strings.Contains(byName["schedule-detail"], "OnCalendar=Mon..Fri 09:00") {
		t.Errorf("schedule-detail snapshot should show the generated unit:\n%s", byName["schedule-detail"])
	}
}

func TestSnapshotsWithoutItemsFallBackToFixtures(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// `exq demo --snapshot --empty` has no commands to render, so every
	// item-dependent state has to stand on built-in fixtures.
	byName := map[string]string{}
	for _, s := range Snapshots(st, nil) {
		if s.View == "" {
			t.Errorf("snapshot %q rendered empty", s.Name)
		}
		byName[s.Name] = s.View
	}
	if !strings.Contains(byName["browse"], "sample-command") {
		t.Errorf("browse snapshot should use the built-in fixture:\n%s", byName["browse"])
	}
	if !strings.Contains(byName["args-form"], "env") {
		t.Errorf("args-form snapshot should use the argful fixture:\n%s", byName["args-form"])
	}
}

func TestSnapshotsUseAnItemThatDeclaresArgs(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	items := []command.Command{
		{Name: "plain", Description: "no arguments"},
		{Name: "deploy", Args: []command.Arg{{Key: "target", Description: "where to deploy"}}},
	}

	byName := map[string]string{}
	for _, s := range Snapshots(st, items) {
		byName[s.Name] = s.View
	}
	// The form snapshot picks the first argful command rather than the
	// built-in fixture.
	if !strings.Contains(byName["args-form"], "target") {
		t.Errorf("args-form snapshot should use the real argful command:\n%s", byName["args-form"])
	}
}
