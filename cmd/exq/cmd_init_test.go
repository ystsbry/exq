package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ystsbry/exq/internal/store"
)

func TestInitCommandCreatesDirsAndExclude(t *testing.T) {
	repo := newRepo(t)

	out, err := run(t, "", "init")
	if err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{"scripts", "workflows"} {
		path := filepath.Join(repo, ".exq", dir)
		if info, statErr := os.Stat(path); statErr != nil || !info.IsDir() {
			t.Errorf("%s not created: %v", path, statErr)
		}
	}
	if !strings.Contains(out, "Created") {
		t.Errorf("output should report the created directories:\n%s", out)
	}
	if !strings.Contains(out, `Added ".exq/"`) {
		t.Errorf("output should report the exclude entry:\n%s", out)
	}
}

func TestInitCommandIsIdempotent(t *testing.T) {
	newRepo(t)
	if _, err := run(t, "", "init"); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, "", "init")
	if err != nil {
		t.Fatal(err)
	}
	// Re-running says what already exists instead of claiming new work.
	if !strings.Contains(out, "already exists") {
		t.Errorf("second init should report the existing directory:\n%s", out)
	}
	if !strings.Contains(out, `already excludes ".exq/"`) {
		t.Errorf("second init should report the existing exclude entry:\n%s", out)
	}
}

func TestInitCommandReportsMigratedEntries(t *testing.T) {
	repo := newRepo(t)
	st, err := store.Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	addEntry(t, filepath.Join(st.Dir(), "commands"), "old-cmd", "description = \"legacy\"\n")

	out, err := run(t, "", "init")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Migrated old-cmd: commands/ -> scripts/") {
		t.Errorf("output should name the migrated entry:\n%s", out)
	}
}

func TestInitCommandOutsideGitRepoFails(t *testing.T) {
	t.Chdir(t.TempDir())

	if _, err := run(t, "", "init"); err == nil {
		t.Error("init outside a git repository should fail")
	}
}

func TestInitCommandRejectsArgs(t *testing.T) {
	newRepo(t)

	if _, err := run(t, "", "init", "extra"); err == nil {
		t.Error("init takes no positional arguments")
	}
}
