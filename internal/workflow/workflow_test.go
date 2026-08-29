package workflow

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ystsbry/exq/internal/command"
	"github.com/ystsbry/exq/internal/runner"
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
	script := "#!/bin/sh\necho " + name + " >> order.txt\nexit " + strconv.Itoa(exitCode) + "\n"
	if err := os.WriteFile(filepath.Join(dir, command.RunFile), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// addScriptBody writes a script with an arbitrary run.sh body, for the
// cases the order.txt convention of addScript cannot express.
func addScriptBody(t *testing.T, st *store.Store, name, body string) {
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
	res, err := Run(t.Context(), st, wf, Options{Workdir: st.Root, Progress: &progress})
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
	res, err := Run(t.Context(), st, wf, Options{Workdir: st.Root, Progress: &progress})
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
	addRawScript(t, st, "chmodless", "#!/bin/sh\n", 0o644)

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
		{
			name:    "blank step entry",
			wf:      addWorkflow(t, st, "blank", []string{"ok", "   "}),
			wantErr: "empty step entry",
		},
		{
			name:    "non-executable step",
			wf:      addWorkflow(t, st, "unrunnable", []string{"chmodless"}),
			wantErr: "not executable",
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

func TestRunRecordsStepThatCannotStart(t *testing.T) {
	st := newStore(t)
	// Executable, so it passes pre-flight validation, but not a program:
	// exec fails and the step has no exit code to report.
	addRawScript(t, st, "not-a-program", "\x7fELF garbage", 0o755)
	addScript(t, st, "after", 0)
	wf := addWorkflow(t, st, "flow", []string{"not-a-program", "after"})

	res, err := Run(t.Context(), st, wf, Options{Workdir: t.TempDir(), Progress: &bytes.Buffer{}})
	// A step that cannot start is a step failure, not a workflow error.
	if err != nil {
		t.Fatalf("Run() err = %v, want nil", err)
	}
	failed := res.Failed()
	if failed == nil {
		t.Fatal("Failed() = nil, want the unstartable step")
	}
	if failed.Name != "not-a-program" || failed.Err == nil {
		t.Errorf("failed step = %+v, want not-a-program with a start error", failed)
	}
	if res.Steps[1].Status != StatusSkipped {
		t.Errorf("step after a failure = %v, want StatusSkipped", res.Steps[1].Status)
	}

	// The summary explains why, instead of printing a meaningless exit 0.
	summary := Summary(res)
	if !strings.Contains(summary, "✗ not-a-program") {
		t.Errorf("summary should mark the step failed:\n%s", summary)
	}
	if strings.Contains(summary, "exit 0") {
		t.Errorf("summary should show the start error, not an exit code:\n%s", summary)
	}
	if !strings.Contains(summary, "- after") {
		t.Errorf("summary should mark the remaining step skipped:\n%s", summary)
	}
}

func TestSummaryOfEmptyResultIsEmpty(t *testing.T) {
	if got := Summary(&Result{}); got != "" {
		t.Errorf("Summary() = %q, want empty", got)
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

// addRawScript writes a script with the given run.sh body and mode.
func addRawScript(t *testing.T, st *store.Store, name, body string, mode os.FileMode) {
	t.Helper()
	dir := filepath.Join(st.ScriptsDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, command.MetaFile), []byte("description = \""+name+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, command.RunFile), []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func TestRunPassesArgsToSteps(t *testing.T) {
	st := newStore(t)
	addRawScript(t, st, "argdump", "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > args.txt\n", 0o755)
	wf := addWorkflow(t, st, "flow",
		[]string{"argdump ${prefix} literal --p=${prefix}"}, "prefix")

	var progress bytes.Buffer
	res, err := Run(t.Context(), st, wf, Options{Workdir: st.Root, Values: []string{"v v"}, Progress: &progress})
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
	addRawScript(t, st, "argdump", "#!/bin/sh\nprintf '[%s]' \"$1\" > args.txt\n", 0o755)
	wf := addWorkflow(t, st, "flow", []string{"argdump ${prefix}"}, "prefix")

	var progress bytes.Buffer
	res, err := Run(t.Context(), st, wf, Options{Workdir: st.Root, Progress: &progress})
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
	if _, err := Run(t.Context(), st, wf, Options{Workdir: st.Root, Values: []string{"extra"}, Progress: &progress}); err == nil || !strings.Contains(err.Error(), "at most") {
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

func TestRunCallsOnStepPerExecutedStep(t *testing.T) {
	st := newStore(t)
	addScript(t, st, "ok", 0)
	addScript(t, st, "bad", 1)
	addScript(t, st, "after", 0)
	wf := addWorkflow(t, st, "flow", []string{"ok", "bad", "after"})

	var progress bytes.Buffer
	type call struct {
		current, total int
		name           string
	}
	var calls []call
	res, err := Run(t.Context(), st, wf, Options{Workdir: st.Root, Progress: &progress, OnStep: func(current, total int, name string) {
		calls = append(calls, call{current, total, name})
	}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed() == nil {
		t.Fatal("expected a failed step")
	}
	// onStep fires for executed steps only — the step skipped after the
	// failure must not be announced.
	want := []call{{1, 3, "ok"}, {2, 3, "bad"}}
	if len(calls) != len(want) {
		t.Fatalf("onStep calls = %+v, want %+v", calls, want)
	}
	for i, w := range want {
		if calls[i] != w {
			t.Errorf("onStep call %d = %+v, want %+v", i, calls[i], w)
		}
	}
}

func TestRunRoutesStepOutputToTheGivenWriters(t *testing.T) {
	st := newStore(t)
	addScriptBody(t, st, "talker", "#!/bin/sh\necho to-stdout\necho to-stderr >&2\n")
	wf := addWorkflow(t, st, "flow", []string{"talker"})

	var progress, out, errOut bytes.Buffer
	if _, err := Run(t.Context(), st, wf, Options{
		Workdir:  st.Root,
		Progress: &progress,
		Exec:     runner.Options{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut},
	}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "to-stdout") {
		t.Fatalf("stdout = %q, want the step's stdout", got)
	}
	if got := errOut.String(); !strings.Contains(got, "to-stderr") {
		t.Fatalf("stderr = %q, want the step's stderr", got)
	}
	// Progress announcements stay on their own writer, so a caller can
	// separate them from what the steps printed.
	if got := progress.String(); !strings.Contains(got, "[1/1] talker") || strings.Contains(got, "to-stdout") {
		t.Fatalf("progress = %q, want only the step announcement", got)
	}
}

func TestRunStopsAtCancellation(t *testing.T) {
	st := newStore(t)
	addScriptBody(t, st, "slow", "#!/bin/sh\necho up\nsleep 60\n")
	addScriptBody(t, st, "next", "#!/bin/sh\necho next-ran\n")
	wf := addWorkflow(t, st, "flow", []string{"slow", "next"})

	ctx, cancel := context.WithCancel(t.Context())
	out := &lockedBuffer{}
	done := make(chan *Result, 1)
	go func() {
		res, err := Run(ctx, st, wf, Options{
			Workdir:  st.Root,
			Progress: out,
			Exec:     runner.Options{Stdin: strings.NewReader(""), Stdout: out, Stderr: out, Group: true},
		})
		if err != nil {
			t.Error(err)
		}
		done <- res
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "up") {
		if time.Now().After(deadline) {
			t.Fatal("first step never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	select {
	case res := <-done:
		if len(res.Steps) != 2 || res.Steps[1].Status != StatusSkipped {
			t.Fatalf("steps = %+v, want the second one skipped", res.Steps)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled workflow did not return")
	}
	if got := out.String(); strings.Contains(got, "next-ran") {
		t.Fatalf("output = %q, want no step started after the cancellation", got)
	}
}

func TestReportRendersTheSummaryAndExitCode(t *testing.T) {
	wf := command.Command{Name: "flow", Kind: command.KindWorkflow}
	var b bytes.Buffer
	code := Report(&b, wf, &Result{Steps: []StepResult{
		{Name: "ok", Status: StatusSuccess},
		{Name: "bad", Status: StatusFailed, ExitCode: 7},
		{Name: "rest", Status: StatusSkipped},
	}})
	if code != 7 {
		t.Fatalf("code = %d, want the failing step's 7", code)
	}
	if got := b.String(); !strings.Contains(got, "failed at step bad") {
		t.Fatalf("report = %q", got)
	}

	b.Reset()
	if code := Report(&b, wf, &Result{Steps: []StepResult{{Name: "ok", Status: StatusSuccess}}}); code != 0 {
		t.Fatalf("code = %d, want 0 for an all-green run", code)
	}
	if got := b.String(); !strings.Contains(got, "all 1 steps succeeded") {
		t.Fatalf("report = %q", got)
	}

	// A step that never started carries no exit code, but the workflow
	// still has to end non-zero.
	b.Reset()
	if code := Report(&b, wf, &Result{Steps: []StepResult{
		{Name: "bad", Status: StatusFailed, Err: os.ErrNotExist},
	}}); code != 1 {
		t.Fatalf("code = %d, want 1 for a step that never started", code)
	}
}

// lockedBuffer is a bytes.Buffer a test can read while a step is still
// writing into it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}
