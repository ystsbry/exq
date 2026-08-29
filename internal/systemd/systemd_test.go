package systemd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// reply is one canned systemctl answer.
type reply struct {
	code int
	out  string
}

// fakeSystemctl installs a stub systemctl on PATH and points the user
// unit directory at a temporary directory, so tests exercise the real
// command plumbing without touching the machine's systemd.
//
// The stub records every invocation and answers from a table keyed by
// the subcommand (the argument after --user); an unlisted subcommand
// succeeds silently. It returns the client and the path of the call log.
func fakeSystemctl(t *testing.T, replies map[string]reply) (*Client, string) {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(bin, "calls.log")
	table := filepath.Join(bin, "replies.txt")

	var b strings.Builder
	for sub, r := range replies {
		// One record per line: subcommand|exit code|output (\n escaped).
		fmt.Fprintf(&b, "%s|%s|%s\n", sub, strconv.Itoa(r.code), strings.ReplaceAll(r.out, "\n", "\\n"))
	}
	if err := os.WriteFile(table, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + log + "\n" +
		"line=$(grep -m1 \"^$2|\" " + table + " 2>/dev/null || true)\n" +
		"if [ -n \"$line\" ]; then\n" +
		"  out=$(printf '%s' \"$line\" | cut -d'|' -f3)\n" +
		"  [ -n \"$out\" ] && printf '%b\\n' \"$out\"\n" +
		"  exit \"$(printf '%s' \"$line\" | cut -d'|' -f2)\"\n" +
		"fi\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	return c, log
}

// calls returns the systemctl invocations the stub recorded.
func calls(t *testing.T, log string) string {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil {
		return ""
	}
	return string(data)
}

func TestNewUsesTheXDGUnitDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if want := "/cfg/systemd/user"; c.UnitDir != want {
		t.Fatalf("UnitDir = %q, want %q", c.UnitDir, want)
	}
}

func TestNewFallsBackToDotConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/tester")
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if want := "/home/tester/.config/systemd/user"; c.UnitDir != want {
		t.Fatalf("UnitDir = %q, want %q", c.UnitDir, want)
	}
}

func TestAvailableReportsAnUnreachableUserInstance(t *testing.T) {
	c, _ := fakeSystemctl(t, map[string]reply{
		"show": {code: 1, out: "Failed to connect to bus"},
	})
	err := c.Available()
	if err == nil {
		t.Fatal("want an error when systemctl --user cannot reach a bus")
	}
	// WSL2 is the case this repo is developed on; the fix belongs in the
	// message rather than in a wiki somewhere.
	if !strings.Contains(err.Error(), "wsl.conf") {
		t.Fatalf("err = %v, want the WSL2 hint", err)
	}
}

func TestAvailablePassesWithAWorkingInstance(t *testing.T) {
	c, _ := fakeSystemctl(t, map[string]reply{"show": {out: "Version=255"}})
	if err := c.Available(); err != nil {
		t.Fatal(err)
	}
}

func TestRunCarriesStderrIntoTheError(t *testing.T) {
	c, _ := fakeSystemctl(t, map[string]reply{"enable": {code: 1, out: "Unit exqd.service not found."}})
	_, err := c.Run("enable", "--now", "exqd.service")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "Unit exqd.service not found") {
		t.Fatalf("err = %v, want systemctl's own message", err)
	}
}

func TestQueryTreatsANonZeroExitAsTheAnswer(t *testing.T) {
	c, _ := fakeSystemctl(t, map[string]reply{"is-active": {code: 3, out: "inactive"}})
	if got := c.Query("is-active", "exqd.service"); got != "inactive" {
		t.Fatalf("Query() = %q, want inactive", got)
	}
}

func TestQueryFallsBackToUnknown(t *testing.T) {
	c, _ := fakeSystemctl(t, map[string]reply{"is-enabled": {code: 1}})
	if got := c.Query("is-enabled", "exqd.service"); got != "unknown" {
		t.Fatalf("Query() = %q, want unknown", got)
	}
}

func TestShowParsesProperties(t *testing.T) {
	c, log := fakeSystemctl(t, map[string]reply{"show": {out: "Result=success\nExecMainStatus=0"}})
	props, err := c.Show("exq-sched-x.service", "Result", "ExecMainStatus")
	if err != nil {
		t.Fatal(err)
	}
	if props["Result"] != "success" || props["ExecMainStatus"] != "0" {
		t.Fatalf("props = %+v", props)
	}
	if got := calls(t, log); !strings.Contains(got, "--property=Result") {
		t.Fatalf("calls = %q, want the property flags to be passed through", got)
	}
}

func TestUnitLifecycleCommands(t *testing.T) {
	c, log := fakeSystemctl(t, nil)
	path, err := c.WriteUnit("exqd.service", "[Unit]\n")
	if err != nil {
		t.Fatal(err)
	}
	if path != c.UnitPath("exqd.service") {
		t.Fatalf("WriteUnit path = %q", path)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "[Unit]\n" {
		t.Fatalf("unit content = %q, err = %v", data, err)
	}
	for _, err := range []error{
		c.DaemonReload(),
		c.EnableNow("exqd.service"),
		c.Restart("exqd.service"),
		c.DisableNow("exqd.service"),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	joined := calls(t, log)
	for _, want := range []string{"--user daemon-reload", "--user enable --now", "--user restart", "--user disable --now"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("calls = %q, missing %q", joined, want)
		}
	}

	if err := c.RemoveUnit("exqd.service"); err != nil {
		t.Fatal(err)
	}
	// Removal has to be repeatable: `schedule remove` may run over a unit
	// a previous, half-finished run already deleted.
	if err := c.RemoveUnit("exqd.service"); err != nil {
		t.Fatalf("removing a unit twice: %v", err)
	}
}

func TestBinaryPathPrefersTheSiblingOfTheRunningBinary(t *testing.T) {
	// The test binary's own directory stands in for ~/.local/bin.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(filepath.Dir(self), "exq-binarypath-fixture")
	if err := os.WriteFile(sibling, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Skipf("cannot write next to the test binary: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(sibling) })

	if got := BinaryPath("exq-binarypath-fixture"); got != sibling {
		t.Fatalf("BinaryPath() = %q, want the sibling %q", got, sibling)
	}
}

func TestBinaryPathFallsBackToLocalBin(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", "/home/tester")
	if got, want := BinaryPath("exqd"), "/home/tester/.local/bin/exqd"; got != want {
		t.Fatalf("BinaryPath() = %q, want %q", got, want)
	}
}
