package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fakeSystemctl puts a stub systemctl on PATH and points the user unit
// directory at a temporary one, so the daemon commands run their real
// systemctl plumbing without touching the machine's systemd. replies
// maps a subcommand to "exit code, output"; anything unlisted succeeds
// silently. It returns the unit directory and the call log path.
func fakeSystemctl(t *testing.T, replies map[string]string) (unitDir, callLog string) {
	t.Helper()
	bin := t.TempDir()
	callLog = filepath.Join(bin, "calls.log")
	table := filepath.Join(bin, "replies.txt")

	var b strings.Builder
	for sub, out := range replies {
		fmt.Fprintf(&b, "%s|%s\n", sub, out)
	}
	if err := os.WriteFile(table, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + callLog + "\n" +
		"line=$(grep -m1 \"^$2|\" " + table + " 2>/dev/null || true)\n" +
		"if [ -n \"$line\" ]; then printf '%s\\n' \"$(printf '%s' \"$line\" | cut -d'|' -f2)\"; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	return filepath.Join(cfg, "systemd", "user"), callLog
}

func TestDaemonInstallWritesAndStartsTheUnit(t *testing.T) {
	unitDir, callLog := fakeSystemctl(t, map[string]string{"show": "Version=255"})
	startDaemon(t)

	out, err := run(t, "", "daemon", "install")
	if err != nil {
		t.Fatal(err)
	}

	unit, err := os.ReadFile(filepath.Join(unitDir, "exqd.service"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Description=exq background job daemon",
		"Type=simple",
		"ExecStart=",
		"Restart=on-failure",
		"WantedBy=default.target",
	} {
		if !strings.Contains(string(unit), want) {
			t.Fatalf("unit = %q, missing %q", unit, want)
		}
	}
	// A system unit would run jobs as root; exq only ever writes user
	// units, which is what --user in every call encodes.
	log := readFile(t, callLog)
	for _, want := range []string{"--user daemon-reload", "--user enable --now exqd.service"} {
		if !strings.Contains(log, want) {
			t.Fatalf("systemctl calls = %q, missing %q", log, want)
		}
	}
	// The daemon started by startDaemon answers, so the install reports a
	// working socket rather than only a started unit.
	if !strings.Contains(out, "ping:    ok") {
		t.Fatalf("install output = %q, want a successful ping", out)
	}
	if !strings.Contains(out, "loginctl enable-linger") {
		t.Fatalf("install output = %q, want the lingering hint", out)
	}
}

func TestDaemonInstallFailsWhenSystemdIsUnreachable(t *testing.T) {
	// A systemctl that fails `show` is what WSL2 without systemd looks
	// like: the binary is there, the user instance is not.
	bin := t.TempDir()
	script := "#!/bin/sh\n[ \"$2\" = show ] && { echo 'Failed to connect to bus' >&2; exit 1; }\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, err := run(t, "", "daemon", "install")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "wsl.conf") {
		t.Fatalf("err = %v, want the WSL2 guidance", err)
	}
}

func TestDaemonStatusReportsUnitAndSocket(t *testing.T) {
	fakeSystemctl(t, map[string]string{
		"show":       "Version=255",
		"is-enabled": "enabled",
		"is-active":  "active",
	})
	startDaemon(t)

	out, err := run(t, "", "daemon", "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"enabled: enabled", "active:  active", "socket:", "ping:    ok"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status output = %q, missing %q", out, want)
		}
	}
}

func TestDaemonStatusReportsAnUnreachableSocket(t *testing.T) {
	fakeSystemctl(t, map[string]string{
		"show":       "Version=255",
		"is-enabled": "disabled",
		"is-active":  "inactive",
	})
	// No daemon: point the socket somewhere that does not exist.
	sockDir, err := os.MkdirTemp("", "exq")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	t.Setenv("XDG_RUNTIME_DIR", sockDir)

	out, err := run(t, "", "daemon", "status")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "active:  inactive") || !strings.Contains(out, "not reachable") {
		t.Fatalf("status output = %q", out)
	}
}

func TestDaemonRestartRunsSystemctlRestart(t *testing.T) {
	_, callLog := fakeSystemctl(t, map[string]string{"show": "Version=255"})
	startDaemon(t)

	out, err := run(t, "", "daemon", "restart")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, callLog), "--user restart exqd.service") {
		t.Fatalf("systemctl calls = %q", readFile(t, callLog))
	}
	if !strings.Contains(out, "Restarted exqd.service") {
		t.Fatalf("restart output = %q", out)
	}
}

func TestDaemonUnitFileNamesTheResolvedBinary(t *testing.T) {
	unit := daemonUnitFile("/home/tester/.local/bin/exqd")
	if !strings.Contains(unit, "ExecStart=/home/tester/.local/bin/exqd\n") {
		t.Fatalf("unit = %q", unit)
	}
	// Counting the sections is a cheap guard against a malformed unit:
	// systemd would reject one silently at daemon-reload time.
	if got := strings.Count(unit, "["); got != 3 {
		t.Fatalf("unit has %s sections, want 3:\n%s", strconv.Itoa(got), unit)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
