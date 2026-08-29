package main

import (
	"errors"
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
	dir := newRepo(t)

	_, err := openStore(dir)
	if err == nil {
		t.Fatal("openStore() = nil, want an error before `exq init`")
	}
	if !strings.Contains(err.Error(), "exq init") {
		t.Errorf("error %v should point at `exq init`", err)
	}
}

func TestOpenStoreFindsInitializedStore(t *testing.T) {
	want := newStore(t)

	got, err := openStore(want.Root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != want.Root {
		t.Errorf("Root = %q, want %q", got.Root, want.Root)
	}
}

func TestOpenStoreFromWDUsesWorkingDirectory(t *testing.T) {
	want := newStore(t)

	got, err := openStoreFromWD()
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != want.Root {
		t.Errorf("Root = %q, want %q", got.Root, want.Root)
	}
}

func TestExitErrorCarriesItsCode(t *testing.T) {
	err := error(exitError{code: 42})
	got, ok := errors.AsType[exitError](err)
	if !ok {
		t.Fatalf("errors.AsType did not recognize %T", err)
	}
	if got.code != 42 {
		t.Errorf("code = %d, want 42", got.code)
	}
	if !strings.Contains(err.Error(), "42") {
		t.Errorf("Error() = %q, should mention the code", err.Error())
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

	var frame strings.Builder
	var runErr error
	stdout, _ := captureStd(t, func() {
		runErr = executeScript(&frame, st, c, []string{"world"}, herdr.New())
	})
	if runErr != nil {
		t.Fatal(runErr)
	}

	// The script's own output goes to the terminal, clean enough to pipe.
	if got := strings.TrimSpace(stdout); got != "hello world" {
		t.Errorf("script stdout = %q, want %q", got, "hello world")
	}
	// The frame is written to the caller's writer and names the command
	// with its arguments.
	if !strings.Contains(frame.String(), "▶ greet world") {
		t.Errorf("missing the start frame:\n%s", frame.String())
	}
	if !strings.Contains(frame.String(), "✓ greet") {
		t.Errorf("missing the success frame:\n%s", frame.String())
	}
}

func TestExecuteScriptWithoutValuesOmitsArgsFromLabel(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "noop", "", "#!/bin/sh\nexit 0\n")
	c, err := st.Get("noop")
	if err != nil {
		t.Fatal(err)
	}

	var frame strings.Builder
	if err := executeScript(&frame, st, c, nil, herdr.New()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(frame.String(), "▶ noop\n") {
		t.Errorf("frame = %q, want a bare command label", frame.String())
	}
}

func TestExecuteScriptFailureReturnsExitError(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "boom", "", "#!/bin/sh\nexit 3\n")
	c, err := st.Get("boom")
	if err != nil {
		t.Fatal(err)
	}

	var frame strings.Builder
	runErr := executeScript(&frame, st, c, nil, herdr.New())

	// The failure travels up as an exit code instead of ending the process
	// here, so callers and tests can both see it.
	exit, ok := errors.AsType[exitError](runErr)
	if !ok {
		t.Fatalf("executeScript() = %v, want an exitError", runErr)
	}
	if exit.code != 3 {
		t.Errorf("exit code = %d, want 3", exit.code)
	}
	if !strings.Contains(frame.String(), "✗ boom") {
		t.Errorf("missing the failure frame:\n%s", frame.String())
	}
	if !strings.Contains(frame.String(), "exit 3") {
		t.Errorf("failure frame should name the exit code:\n%s", frame.String())
	}
}

func TestExecuteScriptSurfacesStartFailure(t *testing.T) {
	st := newStore(t)
	// run.sh exists but is not executable, so the script never starts —
	// which is a plain error, not an exit code.
	addScriptMode(t, st, "broken", "", "#!/bin/sh\n", 0o644)
	c, err := st.Get("broken")
	if err != nil {
		t.Fatal(err)
	}

	var frame strings.Builder
	runErr := executeScript(&frame, st, c, nil, herdr.New())
	if runErr == nil {
		t.Fatal("executeScript() = nil, want an error for an unrunnable script")
	}
	if _, ok := errors.AsType[exitError](runErr); ok {
		t.Errorf("a script that never started should not report an exit code: %v", runErr)
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

	var report strings.Builder
	var runErr error
	stdout, _ := captureStd(t, func() {
		runErr = executeWorkflow(&report, st, wf, nil, herdr.New())
	})
	if runErr != nil {
		t.Fatal(runErr)
	}

	for _, want := range []string{
		"[1/2] first", "[2/2] second", // progress
		"✓ first", "✓ second", // summary
		"workflow both: all 2 steps succeeded",
	} {
		if !strings.Contains(report.String(), want) {
			t.Errorf("report missing %q:\n%s", want, report.String())
		}
	}
	// The steps' own output still reaches the terminal.
	for _, want := range []string{"step-one", "step-two"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("step output missing %q:\n%s", want, stdout)
		}
	}
}

func TestExecuteWorkflowFailingStepReturnsItsExitCode(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "ok", "", "#!/bin/sh\nexit 0\n")
	addScript(t, st, "boom", "", "#!/bin/sh\nexit 4\n")
	addScript(t, st, "never", "", "#!/bin/sh\nexit 0\n")
	addEntry(t, st.WorkflowsDir(), "flow", "steps = [\"ok\", \"boom\", \"never\"]\n")
	wf, err := st.Get("flow")
	if err != nil {
		t.Fatal(err)
	}

	var report strings.Builder
	runErr := executeWorkflow(&report, st, wf, nil, herdr.New())

	exit, ok := errors.AsType[exitError](runErr)
	if !ok {
		t.Fatalf("executeWorkflow() = %v, want an exitError", runErr)
	}
	if exit.code != 4 {
		t.Errorf("exit code = %d, want the failing step's 4", exit.code)
	}
	if !strings.Contains(report.String(), "workflow flow failed at step boom") {
		t.Errorf("report should name the failing step:\n%s", report.String())
	}
	if !strings.Contains(report.String(), "- never") {
		t.Errorf("report should mark the remaining step skipped:\n%s", report.String())
	}
}

func TestExecuteWorkflowUnstartableStepStillFails(t *testing.T) {
	st := newStore(t)
	// Executable but not a program: the step never starts, so it has no
	// exit code of its own and the workflow has to invent a non-zero one.
	addScript(t, st, "not-a-program", "", "\x7fELF garbage")
	addEntry(t, st.WorkflowsDir(), "flow", "steps = [\"not-a-program\"]\n")
	wf, err := st.Get("flow")
	if err != nil {
		t.Fatal(err)
	}

	var report strings.Builder
	runErr := executeWorkflow(&report, st, wf, nil, herdr.New())

	exit, ok := errors.AsType[exitError](runErr)
	if !ok {
		t.Fatalf("executeWorkflow() = %v, want an exitError", runErr)
	}
	if exit.code != 1 {
		t.Errorf("exit code = %d, want 1", exit.code)
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

	var report strings.Builder
	runErr := executeWorkflow(&report, st, wf, nil, herdr.New())
	if runErr == nil {
		t.Fatal("executeWorkflow() = nil, want a validation error")
	}
	if _, ok := errors.AsType[exitError](runErr); ok {
		t.Errorf("a workflow that never ran should not report an exit code: %v", runErr)
	}
	if !strings.Contains(runErr.Error(), "missing") {
		t.Errorf("error %v should name the unresolvable step", runErr)
	}
}

func TestExecuteDispatchesOnKind(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "solo", "", "#!/bin/sh\nexit 0\n")
	addEntry(t, st.WorkflowsDir(), "flow", "steps = [\"solo\"]\n")

	script, err := st.Get("solo")
	if err != nil {
		t.Fatal(err)
	}
	wf, err := st.Get("flow")
	if err != nil {
		t.Fatal(err)
	}

	// A script gets the ▶/✓ frame on errOut; a workflow gets progress and
	// the summary on out. Neither writes to the other's stream.
	var out, errOut strings.Builder
	if err := execute(&out, &errOut, st, script, nil, herdr.New()); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 || !strings.Contains(errOut.String(), "✓ solo") {
		t.Errorf("script should frame on errOut only: out=%q errOut=%q", out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if err := execute(&out, &errOut, st, wf, nil, herdr.New()); err != nil {
		t.Fatal(err)
	}
	if errOut.Len() != 0 || !strings.Contains(out.String(), "all 1 steps succeeded") {
		t.Errorf("workflow should report on out only: out=%q errOut=%q", out.String(), errOut.String())
	}
}
