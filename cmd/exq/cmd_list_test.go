package main

import (
	"strings"
	"testing"
)

func TestListCommandOnEmptyStore(t *testing.T) {
	st := newStore(t)

	out, err := run(t, "", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No commands under "+st.Dir()) {
		t.Errorf("empty store should say where it looked:\n%s", out)
	}
}

func TestListCommandGroupsByKind(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "build", "description = \"build the binary\"\n", "#!/bin/sh\n")
	addScript(t, st, "test", "description = \"run the tests\"\n", "#!/bin/sh\n")
	addEntry(t, st.WorkflowsDir(), "check",
		"description = \"everything before a PR\"\nsteps = [\"build\", \"test\"]\n")

	out, err := run(t, "", "list")
	if err != nil {
		t.Fatal(err)
	}

	scripts := strings.Index(out, "scripts:")
	workflows := strings.Index(out, "workflows:")
	if scripts < 0 || workflows < 0 {
		t.Fatalf("both section headers should be present:\n%s", out)
	}
	// Scripts come first, matching the store's kind-major order.
	if scripts > workflows {
		t.Errorf("scripts section should precede workflows:\n%s", out)
	}
	if !strings.Contains(out, "build the binary") {
		t.Errorf("descriptions should be listed:\n%s", out)
	}
	if !strings.Contains(out, "steps: build → test") {
		t.Errorf("workflow steps should be listed:\n%s", out)
	}
}

func TestListCommandShowsDeclaredArgs(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "deploy", `description = "deploy it"

[[args]]
key = "env"

[[args]]
key = "service"
`, "#!/bin/sh\n")

	out, err := run(t, "", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "deploy it (args: env, service)") {
		t.Errorf("argument keys should be listed in declaration order:\n%s", out)
	}
}

func TestListCommandAliasLs(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "build", "", "#!/bin/sh\n")

	out, err := run(t, "", "ls")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "build") {
		t.Errorf("`ls` should behave like `list`:\n%s", out)
	}
}

func TestListCommandWithoutInitFails(t *testing.T) {
	newRepo(t)

	if _, err := run(t, "", "list"); err == nil {
		t.Error("list before `exq init` should fail")
	}
}

func TestListCommandSurfacesStoreErrors(t *testing.T) {
	st := newStore(t)
	// The same name in both subdirectories is ambiguous, and listing must
	// say so rather than showing one of them.
	addScript(t, st, "deploy", "", "#!/bin/sh\n")
	addEntry(t, st.WorkflowsDir(), "deploy", "steps = [\"deploy\"]\n")

	_, err := run(t, "", "list")
	if err == nil || !strings.Contains(err.Error(), "unique") {
		t.Errorf("list = %v, want a duplicate-name error", err)
	}
}
