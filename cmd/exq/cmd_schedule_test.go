package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSystemdWithAnalyze extends the systemctl stub with a
// systemd-analyze that rejects the given calendar expressions, so the
// validation path can be exercised.
func fakeSystemdWithAnalyze(t *testing.T, replies map[string]string, badCalendars ...string) (unitDir string) {
	t.Helper()
	unitDir, callLog := fakeSystemctl(t, replies)
	bin := filepath.Dir(callLog)
	script := "#!/bin/sh\ncase \"$2\" in\n"
	for _, bad := range badCalendars {
		script += "  '" + bad + "') echo 'Failed to parse calendar specification' >&2; exit 1;;\n"
	}
	script += "esac\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "systemd-analyze"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return unitDir
}

func TestScheduleAddRegistersATimer(t *testing.T) {
	unitDir := fakeSystemdWithAnalyze(t, map[string]string{"show": "Version=255"})
	st := newStore(t)
	addScript(t, st, "nightly", "description = \"nightly\"\n", "#!/bin/sh\n")

	out, err := run(t, "", "schedule", "add", "nightly", "--on-calendar", "Mon..Fri 09:00")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Scheduled nightly as ") {
		t.Fatalf("add output = %q", out)
	}
	units, err := os.ReadDir(unitDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, u := range units {
		names = append(names, u.Name())
	}
	if len(names) != 2 {
		t.Fatalf("units = %v, want a timer and a service", names)
	}
}

func TestScheduleAddRequiresACalendarExpression(t *testing.T) {
	fakeSystemdWithAnalyze(t, map[string]string{"show": "Version=255"})
	st := newStore(t)
	addScript(t, st, "nightly", "", "#!/bin/sh\n")

	_, err := run(t, "", "schedule", "add", "nightly")
	if err == nil || !strings.Contains(err.Error(), "--on-calendar") {
		t.Fatalf("err = %v, want it to ask for --on-calendar", err)
	}
}

func TestScheduleAddRejectsAnInvalidExpression(t *testing.T) {
	fakeSystemdWithAnalyze(t, map[string]string{"show": "Version=255"}, "every so often")
	st := newStore(t)
	addScript(t, st, "nightly", "", "#!/bin/sh\n")

	_, err := run(t, "", "schedule", "add", "nightly", "--on-calendar", "every so often")
	if err == nil || !strings.Contains(err.Error(), "invalid OnCalendar") {
		t.Fatalf("err = %v, want the calendar validation to fail", err)
	}
}

func TestScheduleAddRejectsAnUnknownCommand(t *testing.T) {
	fakeSystemdWithAnalyze(t, map[string]string{"show": "Version=255"})
	newStore(t)

	// The timer would otherwise fire forever with nothing to run.
	if _, err := run(t, "", "schedule", "add", "ghost", "--on-calendar", "daily"); err == nil {
		t.Fatal("want an error for an unknown command")
	}
}

func TestScheduleListShowsRegisteredSchedules(t *testing.T) {
	fakeSystemdWithAnalyze(t, map[string]string{
		"show": "Version=255\nNextElapseUSecRealtime=1787000000000000\nResult=success\nExecMainStartTimestamp=Fri 2026-08-28 09:00:00 JST",
	})
	st := newStore(t)
	addScript(t, st, "nightly", "", "#!/bin/sh\n")
	if _, err := run(t, "", "schedule", "add", "nightly", "--on-calendar", "daily"); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, "", "schedule", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ID", "nightly", "daily", "success"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list output = %q, missing %q", out, want)
		}
	}
}

func TestScheduleListReportsAnEmptyRegistry(t *testing.T) {
	fakeSystemdWithAnalyze(t, nil)
	out, err := run(t, "", "schedule", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No schedules yet") {
		t.Fatalf("list output = %q", out)
	}
}

func TestScheduleListWarnsAboutAMissingWorkdir(t *testing.T) {
	fakeSystemdWithAnalyze(t, map[string]string{"show": "Version=255"})
	st := newStore(t)
	addScript(t, st, "nightly", "", "#!/bin/sh\n")
	if _, err := run(t, "", "schedule", "add", "nightly", "--on-calendar", "daily"); err != nil {
		t.Fatal(err)
	}
	// newStore chdir'd into the repository; leave it before deleting it.
	t.Chdir(t.TempDir())
	if err := os.RemoveAll(st.Root); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, "", "schedule", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "(!)") || !strings.Contains(out, "exq schedule remove") {
		t.Fatalf("list output = %q, want the stale-schedule warning", out)
	}
}

func TestScheduleRemoveAsksBeforeDeleting(t *testing.T) {
	unitDir := fakeSystemdWithAnalyze(t, map[string]string{"show": "Version=255"})
	st := newStore(t)
	addScript(t, st, "nightly", "", "#!/bin/sh\n")
	added, err := run(t, "", "schedule", "add", "nightly", "--on-calendar", "daily")
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Fields(strings.SplitN(added, "\n", 2)[0])[3]

	if out, err := run(t, "n\n", "schedule", "remove", id); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(out, "Cancelled") {
		t.Fatalf("declined removal printed %q", out)
	}
	if units, _ := os.ReadDir(unitDir); len(units) != 2 {
		t.Fatalf("units after a declined removal = %v, want both kept", units)
	}

	out, err := run(t, "y\n", "schedule", "remove", id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Removed schedule "+id) {
		t.Fatalf("remove output = %q", out)
	}
	if units, _ := os.ReadDir(unitDir); len(units) != 0 {
		t.Fatalf("units left behind: %v", units)
	}
}

func TestScheduleRemoveRejectsAnUnknownID(t *testing.T) {
	fakeSystemdWithAnalyze(t, nil)
	if _, err := run(t, "y\n", "schedule", "remove", "no-such-schedule"); err == nil {
		t.Fatal("want an error for an unknown schedule")
	}
}
