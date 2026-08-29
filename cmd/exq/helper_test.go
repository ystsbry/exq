package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ystsbry/exq/internal/command"
	"github.com/ystsbry/exq/internal/store"
)

// newRepo creates a temporary git repository and makes it the working
// directory for the test, so the commands under test — which all resolve
// the store from the cwd — operate on it.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	t.Chdir(dir)
	return dir
}

// newStore creates an initialized store in a temporary git repository and
// makes it the working directory.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(newRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Init(); err != nil {
		t.Fatal(err)
	}
	return st
}

// addScript writes a runnable script with the given command.toml body and
// run.sh body.
func addScript(t *testing.T, st *store.Store, name, meta, script string) {
	t.Helper()
	addScriptMode(t, st, name, meta, script, 0o755)
}

// addScriptMode is addScript with an explicit run.sh mode, so tests can
// create a script that exists but cannot be executed.
func addScriptMode(t *testing.T, st *store.Store, name, meta, script string, mode os.FileMode) {
	t.Helper()
	addEntry(t, st.ScriptsDir(), name, meta)
	path := filepath.Join(st.ScriptsDir(), name, command.RunFile)
	if err := os.WriteFile(path, []byte(script), mode); err != nil {
		t.Fatal(err)
	}
}

// addEntry writes a command directory holding only command.toml, which is
// all a workflow needs.
func addEntry(t *testing.T, base, name, meta string) {
	t.Helper()
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, command.MetaFile), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
}

// run executes the root command with args, returning everything it wrote to
// its own output and error writers. stdin, when non-empty, answers prompts.
func run(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetIn(strings.NewReader(stdin))
	err := root.Execute()
	return buf.String(), err
}

// captureStd runs fn with os.Stdout and os.Stderr redirected to pipes and
// returns what each received. The run frame (▶ / ✓) and a script's own
// output go to the process streams, not to the cobra command's writers.
func captureStd(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	restore := func() { os.Stdout, os.Stderr = origOut, origErr }
	defer restore()

	// Drain both pipes concurrently: a script writing more than the pipe
	// buffer would otherwise block forever.
	var outBuf, errBuf strings.Builder
	var wg sync.WaitGroup
	wg.Go(func() { _, _ = io.Copy(&outBuf, outR) })
	wg.Go(func() { _, _ = io.Copy(&errBuf, errR) })

	fn()

	restore()
	_ = outW.Close()
	_ = errW.Close()
	wg.Wait()
	_ = outR.Close()
	_ = errR.Close()
	return outBuf.String(), errBuf.String()
}

// subcommand returns the registered subcommand with the given name.
func subcommand(t *testing.T, root *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, c := range root.Commands() {
		if c.Name() == name {
			return c
		}
	}
	t.Fatalf("subcommand %q is not registered", name)
	return nil
}
