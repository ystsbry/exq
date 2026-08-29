package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ystsbry/exq/internal/command"
)

func TestNewDemoStorePopulatesFixtures(t *testing.T) {
	st, cleanup, err := newDemoStore(false)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	items, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]command.Command{}
	for _, it := range items {
		byName[it.Name] = it
	}
	for _, s := range sampleCommands {
		it, ok := byName[s.name]
		if !ok {
			t.Errorf("sample script %q not discovered", s.name)
			continue
		}
		if it.Kind != command.KindScript {
			t.Errorf("%q kind = %v, want KindScript", s.name, it.Kind)
		}
		// The demo has to be actually runnable, not just listed.
		if err := it.Runnable(); err != nil {
			t.Errorf("sample script %q is not runnable: %v", s.name, err)
		}
	}
	for _, w := range sampleWorkflows {
		it, ok := byName[w.name]
		if !ok {
			t.Errorf("sample workflow %q not discovered", w.name)
			continue
		}
		if it.Kind != command.KindWorkflow {
			t.Errorf("%q kind = %v, want KindWorkflow", w.name, it.Kind)
		}
		if len(it.Steps) == 0 {
			t.Errorf("sample workflow %q declares no steps", w.name)
		}
	}
}

func TestNewDemoStoreEmpty(t *testing.T) {
	st, cleanup, err := newDemoStore(true)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	items, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("--empty should populate nothing, got %+v", items)
	}
}

func TestNewDemoStoreCleanupRemovesTempDir(t *testing.T) {
	st, cleanup, err := newDemoStore(false)
	if err != nil {
		t.Fatal(err)
	}
	// The demo must never touch a real .exq: it lives in its own temp dir.
	if !strings.HasPrefix(st.Root, os.TempDir()) {
		t.Errorf("demo store root = %q, want a path under %q", st.Root, os.TempDir())
	}

	cleanup()
	if _, err := os.Stat(st.Root); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("cleanup should remove %s: %v", st.Root, err)
	}
}

func TestDemoSnapshotRendersEveryState(t *testing.T) {
	out, err := run(t, "", "demo", "--snapshot")
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"browse", "browse-workflows", "browse-empty",
		"confirm-delete", "args-form", "error", "workflow-summary",
	} {
		if !strings.Contains(out, "=== "+name+" ===") {
			t.Errorf("snapshot %q missing:\n%s", name, out)
		}
	}
	// The fixtures should be visible in the rendered states.
	if !strings.Contains(out, "deploy-local") {
		t.Errorf("browse snapshot should list the sample commands:\n%s", out)
	}
	// The workflow summary fixture shows all three step outcomes at once.
	for _, want := range []string{"✓ reset-db", "✗ broken-step", "- tail-logs"} {
		if !strings.Contains(out, want) {
			t.Errorf("workflow summary missing %q:\n%s", want, out)
		}
	}
}

func TestDemoSnapshotWithEmptyStore(t *testing.T) {
	out, err := run(t, "", "demo", "--snapshot", "--empty")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no scripts yet") {
		t.Errorf("empty demo should render the empty-state hint:\n%s", out)
	}
	// Item-dependent states still render, from built-in fixtures.
	if !strings.Contains(out, "=== args-form ===") {
		t.Errorf("args-form snapshot missing:\n%s", out)
	}
}

func TestDemoSnapshotNeedsNoExqDirectory(t *testing.T) {
	// Not a git repository and no .exq anywhere: the demo builds its own.
	dir := t.TempDir()
	t.Chdir(dir)

	if _, err := run(t, "", "demo", "--snapshot"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".exq")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("demo must not create .exq in the working directory: %v", err)
	}
}

func TestDemoCommandRejectsArgs(t *testing.T) {
	if _, err := run(t, "", "demo", "extra"); err == nil {
		t.Error("demo takes no positional arguments")
	}
}
