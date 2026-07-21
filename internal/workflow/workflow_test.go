package workflow

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ystsbry/exq/internal/command"
	"github.com/ystsbry/exq/internal/store"
)

// newStore creates an initialized store inside a temporary git repository.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Init(); err != nil {
		t.Fatal(err)
	}
	return st
}

// addScript writes a script whose body appends its name to order.txt and
// exits with the given code.
func addScript(t *testing.T, st *store.Store, name string, exitCode int) {
	t.Helper()
	dir := filepath.Join(st.ScriptsDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := "description = \"" + name + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, command.MetaFile), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho " + name + " >> order.txt\nexit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(filepath.Join(dir, command.RunFile), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func itoa(n int) string {
	return string(rune('0' + n))
}

// addWorkflow writes a workflow with the given steps and declared args.
func addWorkflow(t *testing.T, st *store.Store, name string, steps []string, argKeys ...string) command.Command {
	t.Helper()
	dir := filepath.Join(st.WorkflowsDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// NOTE: steps must precede [[args]] — a top-level key after a TOML
	// table would be parsed as part of that table.
	var b strings.Builder
	b.WriteString("description = \"" + name + "\"\n")
	b.WriteString("steps = [")
	for i, s := range steps {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("\"" + s + "\"")
	}
	b.WriteString("]\n")
	for _, k := range argKeys {
		b.WriteString("[[args]]\nkey = \"" + k + "\"\n")
	}
	if err := os.WriteFile(filepath.Join(dir, command.MetaFile), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := st.Get(name)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRunSequentialSuccess(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "one", 0)
	addScript(t, st, "two", 0)
	wf := addWorkflow(t, st, "flow", []string{"one", "two"})

	var progress bytes.Buffer
	res, err := Run(st, wf, st.Root, nil, &progress)
	if err != nil {
		t.Fatal(err)
	}
	if f := res.Failed(); f != nil {
		t.Fatalf("unexpected failure: %+v", f)
	}
	for i, want := range []Status{StatusSuccess, StatusSuccess} {
		if res.Steps[i].Status != want {
			t.Errorf("step %d status = %v, want %v", i, res.Steps[i].Status, want)
		}
	}

	data, err := os.ReadFile(filepath.Join(st.Root, "order.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "one\ntwo\n" {
		t.Errorf("execution order wrong: %q", data)
	}
	if got := progress.String(); !strings.Contains(got, "[1/2] one") || !strings.Contains(got, "[2/2] two") {
		t.Errorf("progress output wrong:\n%s", got)
	}

	sum := Summary(res)
	if strings.Count(sum, "✓") != 2 || strings.Contains(sum, "✗") {
		t.Errorf("summary wrong:\n%s", sum)
	}
}

func TestRunFailFastSkipsRemaining(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "ok", 0)
	addScript(t, st, "bad", 3)
	addScript(t, st, "after", 0)
	wf := addWorkflow(t, st, "flow", []string{"ok", "bad", "after"})

	var progress bytes.Buffer
	res, err := Run(st, wf, st.Root, nil, &progress)
	if err != nil {
		t.Fatal(err)
	}
	want := []Status{StatusSuccess, StatusFailed, StatusSkipped}
	for i, w := range want {
		if res.Steps[i].Status != w {
			t.Errorf("step %d status = %v, want %v", i, res.Steps[i].Status, w)
		}
	}
	f := res.Failed()
	if f == nil || f.Name != "bad" || f.ExitCode != 3 {
		t.Fatalf("Failed() = %+v, want bad/exit3", f)
	}

	// The skipped step must not have run.
	data, _ := os.ReadFile(filepath.Join(st.Root, "order.txt"))
	if string(data) != "ok\nbad\n" {
		t.Errorf("execution order wrong: %q", data)
	}
	if strings.Contains(progress.String(), "after") {
		t.Errorf("skipped step should not be announced:\n%s", progress.String())
	}

	sum := Summary(res)
	for _, wantStr := range []string{"✓ ok", "✗ bad", "(exit 3)", "- after", "(skipped)"} {
		if !strings.Contains(sum, wantStr) {
			t.Errorf("summary missing %q:\n%s", wantStr, sum)
		}
	}
}

func TestResolveValidation(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "ok", 0)
	addWorkflow(t, st, "inner", []string{"ok"})

	tests := []struct {
		name    string
		wf      command.Command
		wantErr string
	}{
		{
			name:    "no steps",
			wf:      addWorkflow(t, st, "empty", nil),
			wantErr: "has no steps",
		},
		{
			name:    "unknown step",
			wf:      addWorkflow(t, st, "missing", []string{"nope"}),
			wantErr: "not found",
		},
		{
			name:    "nested workflow",
			wf:      addWorkflow(t, st, "nested", []string{"inner"}),
			wantErr: "nesting is not supported",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Resolve(st, tt.wf)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Resolve error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestResolveRejectsRunFileInWorkflow(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "ok", 0)
	wf := addWorkflow(t, st, "mixed", []string{"ok"})
	if err := os.WriteFile(filepath.Join(wf.Dir, command.RunFile), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(st, wf); err == nil || !strings.Contains(err.Error(), "must not have") {
		t.Errorf("expected run.sh rejection, got %v", err)
	}
}

// addRawScript writes a script with the given body.
func addRawScript(t *testing.T, st *store.Store, name, body string) {
	t.Helper()
	dir := filepath.Join(st.ScriptsDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, command.MetaFile), []byte("description = \""+name+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, command.RunFile), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestRunPassesArgsToSteps(t *testing.T) {
	st := newStore(t)
	addRawScript(t, st, "argdump", "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > args.txt\n")
	wf := addWorkflow(t, st, "flow",
		[]string{"argdump ${prefix} literal --p=${prefix}"}, "prefix")

	var progress bytes.Buffer
	res, err := Run(st, wf, st.Root, []string{"v v"}, &progress)
	if err != nil {
		t.Fatal(err)
	}
	if f := res.Failed(); f != nil {
		t.Fatalf("unexpected failure: %+v", f)
	}
	data, err := os.ReadFile(filepath.Join(st.Root, "args.txt"))
	if err != nil {
		t.Fatal(err)
	}
	// A whole-token placeholder keeps the spaced value as one argument;
	// literals pass through; embedded placeholders are substituted in place.
	want := "v v\nliteral\n--p=v v\n"
	if string(data) != want {
		t.Errorf("step argv:\ngot  %q\nwant %q", data, want)
	}
}

func TestRunMissingValueBecomesEmpty(t *testing.T) {
	st := newStore(t)
	addRawScript(t, st, "argdump", "#!/bin/sh\nprintf '[%s]' \"$1\" > args.txt\n")
	wf := addWorkflow(t, st, "flow", []string{"argdump ${prefix}"}, "prefix")

	var progress bytes.Buffer
	res, err := Run(st, wf, st.Root, nil, &progress)
	if err != nil {
		t.Fatal(err)
	}
	if f := res.Failed(); f != nil {
		t.Fatalf("unexpected failure: %+v", f)
	}
	data, _ := os.ReadFile(filepath.Join(st.Root, "args.txt"))
	if string(data) != "[]" {
		t.Errorf("missing value should become empty arg, got %q", data)
	}
}

func TestResolveRejectsUndeclaredPlaceholder(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "ok", 0)
	wf := addWorkflow(t, st, "flow", []string{"ok ${nope}"})
	if _, err := Resolve(st, wf); err == nil || !strings.Contains(err.Error(), "not declared") {
		t.Errorf("expected undeclared placeholder error, got %v", err)
	}
}

func TestRunRejectsTooManyValues(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "ok", 0)
	wf := addWorkflow(t, st, "flow", []string{"ok"})
	var progress bytes.Buffer
	if _, err := Run(st, wf, st.Root, []string{"extra"}, &progress); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Errorf("expected too-many-values error, got %v", err)
	}
}

func TestResolveRejectsNonWorkflow(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "ok", 0)
	c, err := st.Get("ok")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(st, c); err == nil {
		t.Error("expected error for non-workflow")
	}
}
