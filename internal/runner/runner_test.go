package runner

import (
	"os"
	"path/filepath"
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
