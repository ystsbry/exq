package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSplitRunArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		dash    int
		wantN   string
		wantV   []string
		wantErr bool
	}{
		{name: "name only", args: []string{"deploy"}, dash: -1, wantN: "deploy"},
		{name: "values after dash", args: []string{"deploy", "prod", "a b"}, dash: 1,
			wantN: "deploy", wantV: []string{"prod", "a b"}},
		{name: "empty value after dash", args: []string{"deploy", ""}, dash: 1,
			wantN: "deploy", wantV: []string{""}},
		{name: "extra args without dash", args: []string{"deploy", "prod"}, dash: -1, wantErr: true},
		{name: "dash before name", args: []string{"prod"}, dash: 0, wantErr: true},
		{name: "two names before dash", args: []string{"a", "b", "v"}, dash: 2, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, v, err := splitRunArgs(tt.args, tt.dash)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got name=%q values=%v", n, v)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if n != tt.wantN || !reflect.DeepEqual(v, tt.wantV) {
				t.Errorf("got (%q, %v), want (%q, %v)", n, v, tt.wantN, tt.wantV)
			}
		})
	}
}

func TestRunCommandExecutesScript(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "greet", "", "#!/bin/sh\necho hello\n")

	var out string
	var err error
	stdout, _ := captureStd(t, func() {
		out, err = run(t, "", "run", "greet")
	})
	if err != nil {
		t.Fatal(err)
	}
	// The script writes to the terminal; the frame goes to the command's
	// own error writer.
	if !strings.Contains(stdout, "hello") {
		t.Errorf("script output missing:\n%s", stdout)
	}
	if !strings.Contains(out, "✓ greet") {
		t.Errorf("success frame missing:\n%s", out)
	}
}

func TestRunCommandPropagatesTheScriptsExitCode(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "boom", "", "#!/bin/sh\nexit 7\n")

	var out string
	var err error
	captureStd(t, func() {
		out, err = run(t, "", "run", "boom")
	})

	exit, ok := errors.AsType[exitError](err)
	if !ok {
		t.Fatalf("run = %v, want an exitError", err)
	}
	if exit.code != 7 {
		t.Errorf("exit code = %d, want 7", exit.code)
	}
	if !strings.Contains(out, "✗ boom") {
		t.Errorf("failure frame missing:\n%s", out)
	}
}

func TestRunCommandPassesValuesAfterDash(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "argdump", `description = "echo its arguments"

[[args]]
key = "env"
`, "#!/bin/sh\nfor a in \"$@\"; do printf '[%s]' \"$a\"; done\necho\n")

	var err error
	stdout, _ := captureStd(t, func() {
		_, err = run(t, "", "run", "argdump", "--", "prod", "a b")
	})
	if err != nil {
		t.Fatal(err)
	}
	// Values reach run.sh verbatim: spaces stay inside one argument.
	if !strings.Contains(stdout, "[prod][a b]") {
		t.Errorf("arguments not passed through:\n%s", stdout)
	}
}

func TestRunCommandExecutesWorkflow(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "first", "", "#!/bin/sh\necho one\n")
	addEntry(t, st.WorkflowsDir(), "flow", "steps = [\"first\"]\n")

	var out string
	var err error
	captureStd(t, func() {
		out, err = run(t, "", "run", "flow")
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "workflow flow: all 1 steps succeeded") {
		t.Errorf("workflow summary missing:\n%s", out)
	}
}

func TestRunCommandUnknownNameFails(t *testing.T) {
	newStore(t)

	if _, err := run(t, "", "run", "nope"); err == nil {
		t.Error("running a missing command should fail")
	}
}

func TestRunCommandRequiresAName(t *testing.T) {
	newStore(t)

	if _, err := run(t, "", "run"); err == nil {
		t.Error("run without a command name should fail")
	}
}

func TestRunCommandRejectsExtraArgsWithoutDash(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "greet", "", "#!/bin/sh\n")

	_, err := run(t, "", "run", "greet", "prod")
	if err == nil {
		t.Fatal("extra positionals without \"--\" should fail")
	}
	// The message has to show the corrected invocation.
	if !strings.Contains(err.Error(), `exq run greet -- prod`) {
		t.Errorf("error %v should suggest the \"--\" form", err)
	}
}
