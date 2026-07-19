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
	if info, err := os.Stat(st.CommandsDir()); err != nil || !info.IsDir() {
		t.Errorf("commands dir not created: %v", err)
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

// addCommand writes a minimal command directory for tests.
func addCommand(t *testing.T, st *Store, name, description string) {
	t.Helper()
	dir := filepath.Join(st.CommandsDir(), name)
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
	dir := filepath.Join(st.CommandsDir(), "broken")
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
