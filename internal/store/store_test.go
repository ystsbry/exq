package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ystsbry/exq/internal/command"
)

// newRepo creates a temporary git repository and returns its path.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return dir
}

func TestInitCreatesDirAndExclude(t *testing.T) {
	repo := newRepo(t)
	st, err := Open(repo)
	if err != nil {
		t.Fatal(err)
	}

	res, err := st.Init()
	if err != nil {
		t.Fatal(err)
	}
	if !res.CreatedDir {
		t.Error("expected CreatedDir=true on first init")
	}
	if !res.UpdatedExclude {
		t.Error("expected UpdatedExclude=true on first init")
	}
	for _, dir := range []string{st.ScriptsDir(), st.WorkflowsDir()} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Errorf("%s not created: %v", dir, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), ".exq/") {
		t.Errorf("exclude file missing .exq/ entry:\n%s", data)
	}
}

func TestInitIsIdempotent(t *testing.T) {
	repo := newRepo(t)
	st, _ := Open(repo)
	if _, err := st.Init(); err != nil {
		t.Fatal(err)
	}
	res, err := st.Init()
	if err != nil {
		t.Fatal(err)
	}
	if res.CreatedDir || res.UpdatedExclude {
		t.Errorf("second init should be a no-op, got %+v", res)
	}

	data, _ := os.ReadFile(res.ExcludePath)
	if got := strings.Count(string(data), ".exq/"); got != 1 {
		t.Errorf("exclude entry duplicated: %d occurrences\n%s", got, data)
	}
}

func TestInitOutsideGitRepoFails(t *testing.T) {
	st, _ := Open(t.TempDir())
	if _, err := st.Init(); err == nil {
		t.Error("expected error outside a git repository")
	}
}

// addEntry writes a minimal command directory under base for tests.
func addEntry(t *testing.T, base, name, description string) {
	t.Helper()
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := "description = " + `"` + description + `"` + "\n"
	if err := os.WriteFile(filepath.Join(dir, command.MetaFile), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho hello\n"
	if err := os.WriteFile(filepath.Join(dir, command.RunFile), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// addCommand writes a minimal script for tests.
func addCommand(t *testing.T, st *Store, name, description string) {
	t.Helper()
	addEntry(t, st.ScriptsDir(), name, description)
}

func TestListAndGet(t *testing.T) {
	repo := newRepo(t)
	st, _ := Open(repo)
	if _, err := st.Init(); err != nil {
		t.Fatal(err)
	}
	addCommand(t, st, "bravo", "second")
	addCommand(t, st, "alpha", "first")

	cmds, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 || cmds[0].Name != "alpha" || cmds[1].Name != "bravo" {
		t.Fatalf("unexpected list: %+v", cmds)
	}
	if cmds[0].Description != "first" {
		t.Errorf("metadata not loaded: %+v", cmds[0])
	}

	c, err := st.Get("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Runnable(); err != nil {
		t.Errorf("expected runnable: %v", err)
	}
}

func TestListWithoutInitReturnsEmpty(t *testing.T) {
	st, _ := Open(t.TempDir())
	cmds, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 0 {
		t.Errorf("expected empty list, got %+v", cmds)
	}
}

func TestBrokenMetaStillListed(t *testing.T) {
	repo := newRepo(t)
	st, _ := Open(repo)
	if _, err := st.Init(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(st.ScriptsDir(), "broken")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, command.MetaFile), []byte("not toml ["), 0o644); err != nil {
		t.Fatal(err)
	}

	cmds, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0].Name != "broken" || cmds[0].Description != "" {
		t.Fatalf("broken meta should still list the command: %+v", cmds)
	}
}

func TestRemove(t *testing.T) {
	repo := newRepo(t)
	st, _ := Open(repo)
	if _, err := st.Init(); err != nil {
		t.Fatal(err)
	}
	addCommand(t, st, "doomed", "bye")

	if err := st.Remove("doomed"); err != nil {
		t.Fatal(err)
	}
	if cmds, _ := st.List(); len(cmds) != 0 {
		t.Errorf("command not removed: %+v", cmds)
	}
	if err := st.Remove("doomed"); err == nil {
		t.Error("removing a missing command should fail")
	}
}

func TestRemoveRejectsPathTraversal(t *testing.T) {
	repo := newRepo(t)
	st, _ := Open(repo)
	if _, err := st.Init(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", ".", "..", "../evil", "a/b"} {
		if err := st.Remove(name); err == nil {
			t.Errorf("Remove(%q) should fail", name)
		}
	}
}

func TestInitMigratesLegacyCommands(t *testing.T) {
	repo := newRepo(t)
	st, _ := Open(repo)
	addEntry(t, filepath.Join(st.Dir(), "commands"), "old-cmd", "from legacy layout")

	res, err := st.Init()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Migrated) != 1 || res.Migrated[0] != "old-cmd" {
		t.Fatalf("Migrated = %v, want [old-cmd]", res.Migrated)
	}
	if _, err := os.Stat(filepath.Join(st.Dir(), "commands")); !os.IsNotExist(err) {
		t.Error("legacy commands/ should be removed after migration")
	}

	c, err := st.Get("old-cmd")
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != command.KindScript || c.Description != "from legacy layout" {
		t.Errorf("migrated command wrong: %+v", c)
	}

	// Re-running init after migration is a no-op.
	res, err = st.Init()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Migrated) != 0 {
		t.Errorf("second init should migrate nothing, got %v", res.Migrated)
	}
}

func TestLegacyLayoutErrorsOnListAndGet(t *testing.T) {
	repo := newRepo(t)
	st, _ := Open(repo)
	addEntry(t, filepath.Join(st.Dir(), "commands"), "old-cmd", "legacy")

	if _, err := st.List(); err == nil || !strings.Contains(err.Error(), "exq init") {
		t.Errorf("List should hint at migration, got %v", err)
	}
	if _, err := st.Get("old-cmd"); err == nil || !strings.Contains(err.Error(), "exq init") {
		t.Errorf("Get should hint at migration, got %v", err)
	}
}

func TestListDiscoversWorkflowsAndKinds(t *testing.T) {
	repo := newRepo(t)
	st, _ := Open(repo)
	if _, err := st.Init(); err != nil {
		t.Fatal(err)
	}
	addEntry(t, st.ScriptsDir(), "build", "a script")
	wfDir := filepath.Join(st.WorkflowsDir(), "pre-pr")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := "description = \"a workflow\"\n"
	if err := os.WriteFile(filepath.Join(wfDir, command.MetaFile), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}

	cmds, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 {
		t.Fatalf("expected 2 entries, got %+v", cmds)
	}
	// Kind-major order: scripts first, then workflows.
	if cmds[0].Name != "build" || cmds[0].Kind != command.KindScript {
		t.Errorf("first entry should be the script: %+v", cmds[0])
	}
	if cmds[1].Name != "pre-pr" || cmds[1].Kind != command.KindWorkflow {
		t.Errorf("second entry should be the workflow: %+v", cmds[1])
	}

	wf, err := st.Get("pre-pr")
	if err != nil {
		t.Fatal(err)
	}
	if wf.Kind != command.KindWorkflow {
		t.Errorf("Get kind = %v, want KindWorkflow", wf.Kind)
	}
}

func TestDuplicateNameAcrossKindsFails(t *testing.T) {
	repo := newRepo(t)
	st, _ := Open(repo)
	if _, err := st.Init(); err != nil {
		t.Fatal(err)
	}
	addEntry(t, st.ScriptsDir(), "deploy", "script side")
	addEntry(t, st.WorkflowsDir(), "deploy", "workflow side")

	if _, err := st.List(); err == nil || !strings.Contains(err.Error(), "unique") {
		t.Errorf("List should fail on duplicate name, got %v", err)
	}
	if _, err := st.Get("deploy"); err == nil || !strings.Contains(err.Error(), "unique") {
		t.Errorf("Get should fail on duplicate name, got %v", err)
	}
}
