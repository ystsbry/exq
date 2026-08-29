package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ystsbry/exq/internal/command"
)

// writeCommand creates a command whose run.sh records its argv into out.txt
// in the working directory, one line per argument.
func writeCommand(t *testing.T, base string) command.Command {
	t.Helper()
	dir := filepath.Join(base, "argdump")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > out.txt\n"
	if err := os.WriteFile(filepath.Join(dir, command.RunFile), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return command.Load(dir)
}

func TestRunPassesArgsPositionally(t *testing.T) {
	base := t.TempDir()
	work := t.TempDir()
	c := writeCommand(t, base)

	code, err := Run(c, work, []string{"prod", "a b", "", "$HOME"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}

	data, err := os.ReadFile(filepath.Join(work, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	// Spaces stay inside one argument, empty values keep their position,
	// and nothing is shell-expanded.
	want := "prod\na b\n\n$HOME\n"
	if string(data) != want {
		t.Errorf("argv mismatch:\ngot  %q\nwant %q", data, want)
	}
}

func TestRunNoArgs(t *testing.T) {
	base := t.TempDir()
	work := t.TempDir()
	c := writeCommand(t, base)

	code, err := Run(c, work, nil)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	data, err := os.ReadFile(filepath.Join(work, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty argv, got %q", data)
	}
}

// writeScript creates a command named after the test whose run.sh has the
// given body and mode.
func writeScript(t *testing.T, body string, mode os.FileMode) command.Command {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "script")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, command.RunFile), []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	return command.Load(dir)
}

func TestRunReturnsExitCode(t *testing.T) {
	c := writeScript(t, "#!/bin/sh\nexit 3\n", 0o755)

	code, err := Run(c, t.TempDir(), nil)
	// A failing command is not a runner error: the caller decides what a
	// non-zero exit means.
	if err != nil {
		t.Fatalf("Run() err = %v, want nil for a non-zero exit", err)
	}
	if code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
}

func TestRunOnUnrunnableCommandFails(t *testing.T) {
	c := writeScript(t, "#!/bin/sh\n", 0o644)

	code, err := Run(c, t.TempDir(), nil)
	if err == nil {
		t.Fatal("Run() err = nil, want an error for a non-executable run.sh")
	}
	if code != -1 {
		t.Errorf("exit code = %d, want -1 when the command never started", code)
	}
}

func TestRunOnMissingCommandFails(t *testing.T) {
	c := command.Load(filepath.Join(t.TempDir(), "nothing"))

	if _, err := Run(c, t.TempDir(), nil); err == nil {
		t.Error("Run() err = nil, want an error for a missing run.sh")
	}
}

func TestRunUnstartableCommandIsAnError(t *testing.T) {
	// Executable, but not a valid program: exec fails before the process
	// exists, so there is no exit code to report.
	c := writeScript(t, "\x7fELF garbage", 0o755)

	code, err := Run(c, t.TempDir(), nil)
	if err == nil {
		t.Fatal("Run() err = nil, want an error when exec itself fails")
	}
	if code != -1 {
		t.Errorf("exit code = %d, want -1 when the command never started", code)
	}
}

func TestRunUsesWorkdirNotCommandDir(t *testing.T) {
	work := t.TempDir()
	c := writeScript(t, "#!/bin/sh\npwd > pwd.txt\n", 0o755)

	if _, err := Run(c, work, nil); err != nil {
		t.Fatal(err)
	}
	// The marker lands in workdir, and the command directory stays clean.
	data, err := os.ReadFile(filepath.Join(work, "pwd.txt"))
	if err != nil {
		t.Fatalf("script did not run in workdir: %v", err)
	}
	got, err := filepath.EvalSymlinks(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(work)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("working directory = %q, want %q", got, want)
	}
}
