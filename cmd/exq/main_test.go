package main

import (
	"strings"
	"testing"

	"github.com/ystsbry/exq/internal/herdr"
)

func TestRootCommandRegistersEverySubcommand(t *testing.T) {
	root := newRootCmd()
	for _, name := range []string{"version", "init", "list", "run", "remove", "demo"} {
		subcommand(t, root, name)
	}
}

func TestVersionCommandPrintsBuildInfo(t *testing.T) {
	out, err := run(t, "", "version")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"exq", version, commit, date} {
		if !strings.Contains(out, want) {
			t.Errorf("version output %q missing %q", out, want)
		}
	}
}

func TestRootCommandRejectsUnknownPositionalArgs(t *testing.T) {
	// Bare `exq` opens the TUI; a stray argument is a typo, not a command.
	if _, err := run(t, "", "not-a-subcommand"); err == nil {
		t.Error("expected an error for an unknown positional argument")
	}
}

func TestOpenStoreRequiresInit(t *testing.T) {
	newRepo(t)

	_, err := openStore()
	if err == nil {
		t.Fatal("openStore() = nil, want an error before `exq init`")
	}
	if !strings.Contains(err.Error(), "exq init") {
		t.Errorf("error %v should point at `exq init`", err)
	}
}

func TestOpenStoreFindsInitializedStore(t *testing.T) {
	want := newStore(t)

	got, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != want.Root {
		t.Errorf("Root = %q, want %q", got.Root, want.Root)
	}
}

func TestExecuteScriptFramesOutput(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "greet", "description = \"say hi\"\n",
		"#!/bin/sh\necho \"hello $1\"\n")
	c, err := st.Get("greet")
	if err != nil {
		t.Fatal(err)
	}

	var runErr error
	stdout, stderr := captureStd(t, func() {
		runErr = executeScript(st, c, []string{"world"}, herdr.New())
	})
	if runErr != nil {
		t.Fatal(runErr)
	}

	// The script's own output stays on stdout, clean enough to pipe.
	if got := strings.TrimSpace(stdout); got != "hello world" {
		t.Errorf("stdout = %q, want %q", got, "hello world")
	}
	// The frame goes to stderr and names the command with its arguments.
	if !strings.Contains(stderr, "▶ greet world") {
		t.Errorf("stderr missing the start frame:\n%s", stderr)
	}
	if !strings.Contains(stderr, "✓ greet") {
		t.Errorf("stderr missing the success frame:\n%s", stderr)
	}
}

func TestExecuteScriptWithoutValuesOmitsArgsFromLabel(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "noop", "", "#!/bin/sh\nexit 0\n")
	c, err := st.Get("noop")
	if err != nil {
		t.Fatal(err)
	}

	_, stderr := captureStd(t, func() {
		if err := executeScript(st, c, nil, herdr.New()); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(stderr, "▶ noop\n") {
		t.Errorf("stderr = %q, want a bare command label", stderr)
	}
}

func TestExecuteScriptSurfacesStartFailure(t *testing.T) {
	st := newStore(t)
	// run.sh exists but is not executable, so the script never starts —
	// which is an error rather than a non-zero exit.
	addScriptMode(t, st, "broken", "", "#!/bin/sh\n", 0o644)
	c, err := st.Get("broken")
	if err != nil {
		t.Fatal(err)
	}

	var runErr error
	captureStd(t, func() {
		runErr = executeScript(st, c, nil, herdr.New())
	})
	if runErr == nil {
		t.Fatal("executeScript() = nil, want an error for an unrunnable script")
	}
	if !strings.Contains(runErr.Error(), "chmod +x") {
		t.Errorf("error %v should carry the chmod hint", runErr)
	}
}

func TestExecuteWorkflowPrintsProgressAndSummary(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "first", "", "#!/bin/sh\necho step-one\n")
	addScript(t, st, "second", "", "#!/bin/sh\necho step-two\n")
	addEntry(t, st.WorkflowsDir(), "both", "steps = [\"first\", \"second\"]\n")
	wf, err := st.Get("both")
	if err != nil {
		t.Fatal(err)
	}

	var runErr error
	stdout, _ := captureStd(t, func() {
		runErr = executeWorkflow(st, wf, nil, herdr.New())
	})
	if runErr != nil {
		t.Fatal(runErr)
	}

	for _, want := range []string{
		"[1/2] first", "[2/2] second", // progress
		"step-one", "step-two", // the steps' own output
		"✓ first", "✓ second", // summary
		"workflow both: all 2 steps succeeded",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestExecuteWorkflowSurfacesValidationFailure(t *testing.T) {
	st := newStore(t)
	// A workflow whose step does not exist fails before anything runs, so
	// it comes back as a plain error rather than a failed step.
	addEntry(t, st.WorkflowsDir(), "broken", "steps = [\"missing\"]\n")
	wf, err := st.Get("broken")
	if err != nil {
		t.Fatal(err)
	}

	var runErr error
	captureStd(t, func() {
		runErr = executeWorkflow(st, wf, nil, herdr.New())
	})
	if runErr == nil {
		t.Fatal("executeWorkflow() = nil, want a validation error")
	}
	if !strings.Contains(runErr.Error(), "missing") {
		t.Errorf("error %v should name the unresolvable step", runErr)
	}
}
