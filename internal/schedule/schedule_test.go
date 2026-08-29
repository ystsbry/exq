package schedule

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ystsbry/exq/internal/systemd"
)

// fakeSystemd installs stub systemctl and systemd-analyze binaries on
// PATH and points the unit directory at a temporary one, so these tests
// drive the real unit-file and systemctl plumbing without touching the
// machine's systemd.
//
// replies maps a systemctl subcommand to the text it prints; `calendar`
// arguments listed in badCalendars make systemd-analyze fail. It returns
// the client and the path of the systemctl call log.
func fakeSystemd(t *testing.T, replies map[string]string, badCalendars ...string) (*systemd.Client, string) {
	t.Helper()
	bin := t.TempDir()
	callLog := filepath.Join(bin, "calls.log")
	table := filepath.Join(bin, "replies.txt")

	var b strings.Builder
	for sub, out := range replies {
		fmt.Fprintf(&b, "%s|%s\n", sub, strings.ReplaceAll(out, "\n", "\\n"))
	}
	if err := os.WriteFile(table, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	systemctl := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + callLog + "\n" +
		"line=$(grep -m1 \"^$2|\" " + table + " 2>/dev/null || true)\n" +
		"if [ -n \"$line\" ]; then printf '%b\\n' \"$(printf '%s' \"$line\" | cut -d'|' -f2)\"; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(systemctl), 0o755); err != nil {
		t.Fatal(err)
	}
	analyze := "#!/bin/sh\ncase \"$2\" in\n"
	for _, bad := range badCalendars {
		analyze += "  '" + bad + "') echo 'Failed to parse calendar specification' >&2; exit 1;;\n"
	}
	analyze += "esac\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "systemd-analyze"), []byte(analyze), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, err := systemd.New()
	if err != nil {
		t.Fatal(err)
	}
	return c, callLog
}

// spec is a schedule registration rooted at a real temporary directory,
// so the workdir checks see something that exists.
func newSpec(t *testing.T, name string, values ...string) Spec {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "myproject")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return Spec{
		Name:       name,
		ProjectDir: dir,
		Workdir:    dir,
		OnCalendar: "Mon..Fri 09:00",
		Values:     values,
	}
}

func readUnit(t *testing.T, sc *systemd.Client, name string) string {
	t.Helper()
	data, err := os.ReadFile(sc.UnitPath(name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAddWritesAndEnablesTheUnitPair(t *testing.T) {
	sc, callLog := fakeSystemd(t, map[string]string{"show": "Version=255"})
	s, err := Add(sc, newSpec(t, "test"))
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "myproject-test" {
		t.Fatalf("id = %q, want myproject-test", s.ID)
	}

	timer := readUnit(t, sc, s.TimerUnit())
	for _, want := range []string{"OnCalendar=Mon..Fri 09:00", "Persistent=true", "WantedBy=timers.target"} {
		if !strings.Contains(timer, want) {
			t.Fatalf("timer = %q, missing %q", timer, want)
		}
	}
	service := readUnit(t, sc, s.ServiceUnit())
	for _, want := range []string{
		"Type=oneshot",
		"Wants=exqd.service",
		"WorkingDirectory=" + s.Workdir,
		"run \"test\" --bg --schedule-id \"myproject-test\"",
		"[X-Exq]",
	} {
		if !strings.Contains(service, want) {
			t.Fatalf("service = %q, missing %q", service, want)
		}
	}

	log, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--user daemon-reload", "--user enable --now " + s.TimerUnit()} {
		if !strings.Contains(string(log), want) {
			t.Fatalf("systemctl calls = %q, missing %q", log, want)
		}
	}
}

func TestAddPassesValuesThroughExecStart(t *testing.T) {
	sc, _ := fakeSystemd(t, map[string]string{"show": "Version=255"})
	s, err := Add(sc, newSpec(t, "deploy", "prod", "a b", ""))
	if err != nil {
		t.Fatal(err)
	}
	service := readUnit(t, sc, s.ServiceUnit())
	// A value with a space has to survive as one argument, and an empty
	// value has to keep its position — the same contract as `exq run --`.
	if !strings.Contains(service, `-- "prod" "a b" ""`) {
		t.Fatalf("service = %q, want the quoted values on ExecStart", service)
	}
}

func TestAddEscapesPercentInUnitValues(t *testing.T) {
	sc, _ := fakeSystemd(t, map[string]string{"show": "Version=255"})
	spec := newSpec(t, "report", "100%")
	spec.OnCalendar = "daily"
	s, err := Add(sc, spec)
	if err != nil {
		t.Fatal(err)
	}
	// A literal % would otherwise be read as the start of a systemd
	// specifier such as %h.
	service := readUnit(t, sc, s.ServiceUnit())
	if !strings.Contains(service, `"100%%"`) {
		t.Fatalf("service = %q, want the %% escaped on ExecStart", service)
	}
	// The X- section is never parsed by systemd, so it keeps the value
	// verbatim — which is what List has to read back.
	if got := s.Values; len(got) != 1 || got[0] != "100%" {
		t.Fatalf("values = %v", got)
	}
	back, err := Get(sc, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Values) != 1 || back.Values[0] != "100%" {
		t.Fatalf("values read back = %v, want [100%%]", back.Values)
	}
}

func TestAddRejectsAnInvalidCalendarWithoutWritingAnything(t *testing.T) {
	sc, _ := fakeSystemd(t, map[string]string{"show": "Version=255"}, "every tuesday-ish")
	spec := newSpec(t, "test")
	spec.OnCalendar = "every tuesday-ish"

	if _, err := Add(sc, spec); err == nil {
		t.Fatal("want an error for an invalid OnCalendar expression")
	}
	entries, err := systemd.ListUnitFiles(sc.UnitDir, unitPrefix+"*")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("units written despite the invalid expression: %v", entries)
	}
}

func TestAddDisambiguatesRepeatedRegistrations(t *testing.T) {
	sc, _ := fakeSystemd(t, map[string]string{"show": "Version=255"})
	spec := newSpec(t, "test")
	first, err := Add(sc, spec)
	if err != nil {
		t.Fatal(err)
	}
	// The same command on a second calendar is legitimate, so the id
	// gains a suffix rather than the registration failing.
	spec.OnCalendar = "daily"
	second, err := Add(sc, spec)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatalf("both registrations got the id %q", first.ID)
	}
	if second.ID != first.ID+"-2" {
		t.Fatalf("second id = %q, want %s-2", second.ID, first.ID)
	}
}

func TestListReconstructsSchedulesFromUnits(t *testing.T) {
	sc, _ := fakeSystemd(t, map[string]string{
		"show": "NextElapseUSecRealtime=1787000000000000\nResult=success\nExecMainStartTimestamp=Fri 2026-08-28 09:00:00 JST",
	})
	spec := newSpec(t, "test", "prod")
	if _, err := Add(sc, spec); err != nil {
		t.Fatal(err)
	}

	schedules, err := List(sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(schedules) != 1 {
		t.Fatalf("listed %d schedules, want 1", len(schedules))
	}
	got := schedules[0]
	if got.Name != "test" || got.ProjectDir != spec.ProjectDir || got.OnCalendar != spec.OnCalendar {
		t.Fatalf("schedule = %+v", got)
	}
	if len(got.Values) != 1 || got.Values[0] != "prod" {
		t.Fatalf("values = %v, want [prod]", got.Values)
	}
	if want := time.UnixMicro(1787000000000000); !got.NextElapse.Equal(want) {
		t.Fatalf("next elapse = %v, want %v", got.NextElapse, want)
	}
	if got.LastResult != "success" {
		t.Fatalf("last result = %q, want success", got.LastResult)
	}
	if got.WorkdirMissing {
		t.Fatal("workdir reported missing although it exists")
	}
}

func TestListFlagsAMissingWorkdir(t *testing.T) {
	sc, _ := fakeSystemd(t, nil)
	spec := newSpec(t, "test")
	if _, err := Add(sc, spec); err != nil {
		t.Fatal(err)
	}
	// The project directory is what disappears when a repository is
	// deleted or moved — the timer stays behind and keeps failing.
	if err := os.RemoveAll(spec.Workdir); err != nil {
		t.Fatal(err)
	}

	schedules, err := List(sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(schedules) != 1 || !schedules[0].WorkdirMissing {
		t.Fatalf("schedules = %+v, want the missing workdir flagged", schedules)
	}
}

func TestListIgnoresUnitsItCannotRead(t *testing.T) {
	sc, _ := fakeSystemd(t, nil)
	// A timer without its service is a half-removed schedule.
	if _, err := sc.WriteUnit(unitPrefix+"orphan.timer", "[Timer]\n"); err != nil {
		t.Fatal(err)
	}
	schedules, err := List(sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(schedules) != 0 {
		t.Fatalf("schedules = %+v, want none", schedules)
	}
}

func TestRemoveDisablesAndDeletesTheUnits(t *testing.T) {
	sc, callLog := fakeSystemd(t, map[string]string{"show": "Version=255"})
	s, err := Add(sc, newSpec(t, "test"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Remove(sc, s.ID); err != nil {
		t.Fatal(err)
	}
	remaining, err := systemd.ListUnitFiles(sc.UnitDir, unitPrefix+"*")
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("units left behind: %v", remaining)
	}
	log, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "--user disable --now "+s.TimerUnit()) {
		t.Fatalf("systemctl calls = %q, want the timer disabled", log)
	}
}

func TestGetAndRemoveRejectUnknownOrUnsafeIDs(t *testing.T) {
	sc, _ := fakeSystemd(t, nil)
	for _, id := range []string{"", "../escape", "no-such-schedule"} {
		if _, err := Get(sc, id); err == nil {
			t.Fatalf("Get(%q): want an error", id)
		}
		if err := Remove(sc, id); err == nil {
			t.Fatalf("Remove(%q): want an error", id)
		}
	}
}

func TestSlugReducesNamesToUnitSafeText(t *testing.T) {
	cases := map[string]string{
		"MyProject":      "myproject",
		"exq":            "exq",
		"my_project.git": "my-project-git",
		"--weird--":      "weird",
		"":               "",
	}
	for in, want := range cases {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}
