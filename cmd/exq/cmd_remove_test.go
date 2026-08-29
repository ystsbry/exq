package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfirmYes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "y", input: "y\n", want: true},
		{name: "uppercase Y", input: "Y\n", want: true},
		{name: "yes", input: "yes\n", want: true},
		{name: "padded yes", input: "  YES  \n", want: true},
		{name: "n", input: "n\n"},
		{name: "empty line defaults to no", input: "\n"},
		{name: "anything else", input: "maybe\n"},
		// A closed stdin (piped, no input) must not be read as consent.
		{name: "no input at all", input: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			got := confirmYes(strings.NewReader(tt.input), &out, "doomed")
			if got != tt.want {
				t.Errorf("confirmYes(%q) = %v, want %v", tt.input, got, tt.want)
			}
			if !strings.Contains(out.String(), `Delete "doomed"? [y/N]`) {
				t.Errorf("prompt not written: %q", out.String())
			}
		})
	}
}

func TestRemoveCommandWithYesFlagSkipsPrompt(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "doomed", "", "#!/bin/sh\n")

	out, err := run(t, "", "remove", "doomed", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Removed doomed") {
		t.Errorf("output should confirm the removal:\n%s", out)
	}
	if strings.Contains(out, "[y/N]") {
		t.Errorf("--yes should skip the prompt:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(st.ScriptsDir(), "doomed")); !os.IsNotExist(statErr) {
		t.Errorf("command directory should be gone: %v", statErr)
	}
}

func TestRemoveCommandConfirmed(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "doomed", "", "#!/bin/sh\n")

	out, err := run(t, "y\n", "remove", "doomed")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Removed doomed") {
		t.Errorf("output should confirm the removal:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(st.ScriptsDir(), "doomed")); !os.IsNotExist(statErr) {
		t.Errorf("command directory should be gone: %v", statErr)
	}
}

func TestRemoveCommandCancelledKeepsCommand(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "keep", "", "#!/bin/sh\n")

	out, err := run(t, "n\n", "remove", "keep")
	// Declining is a normal outcome, not a failure.
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Cancelled.") {
		t.Errorf("output should report the cancellation:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(st.ScriptsDir(), "keep")); statErr != nil {
		t.Errorf("command directory should survive: %v", statErr)
	}
}

func TestRemoveCommandAliasRm(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "doomed", "", "#!/bin/sh\n")

	if _, err := run(t, "", "rm", "doomed", "-y"); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(st.ScriptsDir(), "doomed")); !os.IsNotExist(statErr) {
		t.Errorf("`rm` should behave like `remove`: %v", statErr)
	}
}

func TestRemoveCommandRemovesWorkflows(t *testing.T) {
	st := newStore(t)
	addEntry(t, st.WorkflowsDir(), "flow", "steps = [\"build\"]\n")

	if _, err := run(t, "", "remove", "flow", "-y"); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(st.WorkflowsDir(), "flow")); !os.IsNotExist(statErr) {
		t.Errorf("workflow directory should be gone: %v", statErr)
	}
}

func TestRemoveCommandUnknownNameFails(t *testing.T) {
	newStore(t)

	if _, err := run(t, "", "remove", "nope", "-y"); err == nil {
		t.Error("removing a missing command should fail")
	}
}

func TestRemoveCommandRequiresExactlyOneName(t *testing.T) {
	newStore(t)

	for _, args := range [][]string{{"remove"}, {"remove", "a", "b"}} {
		if _, err := run(t, "", args...); err == nil {
			t.Errorf("%v should fail: remove takes exactly one name", args)
		}
	}
}
