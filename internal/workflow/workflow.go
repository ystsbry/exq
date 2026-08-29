// Package workflow executes a workflow — an ordered composition of
// scripts declared via `steps` in command.toml — sequentially, stopping
// at the first failure and collecting per-step results for the summary.
package workflow

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ystsbry/exq/internal/command"
	"github.com/ystsbry/exq/internal/runner"
	"github.com/ystsbry/exq/internal/store"
)

// placeholderRe matches ${key} references to the workflow's own [[args]]
// inside step argument tokens.
var placeholderRe = regexp.MustCompile(`\$\{([A-Za-z0-9_-]+)\}`)

// Step is one resolved workflow step: the script to run plus its argument
// tokens (placeholders not yet expanded — Run expands them with the
// values supplied at execution time).
type Step struct {
	Command command.Command
	argv    []string
}

// Status is the outcome of one step.
type Status int

const (
	StatusSuccess Status = iota
	StatusFailed
	StatusSkipped
)

// StepResult is the recorded outcome of one executed (or skipped) step.
type StepResult struct {
	Name     string
	Status   Status
	ExitCode int
	Duration time.Duration
	Err      error // non-nil only when the step could not be started at all
}

// Result is the outcome of a whole workflow run.
type Result struct {
	Steps []StepResult
}

// Failed returns the failed step, or nil when every step succeeded.
func (r *Result) Failed() *StepResult {
	for i := range r.Steps {
		if r.Steps[i].Status == StatusFailed {
			return &r.Steps[i]
		}
	}
	return nil
}

// Resolve validates wf and returns its steps as runnable scripts, in
// order. Each steps entry is "name [arg ...]" where args may reference
// the workflow's own [[args]] as ${key}. Resolve fails before anything
// runs: on a workflow without steps, a stray run.sh, an unknown step, a
// workflow step (nesting is not supported), a step whose entrypoint is
// not executable, or a ${key} that is not declared in [[args]].
func Resolve(st *store.Store, wf command.Command) ([]Step, error) {
	if wf.Kind != command.KindWorkflow {
		return nil, fmt.Errorf("%q is not a workflow", wf.Name)
	}
	if len(wf.Steps) == 0 {
		return nil, fmt.Errorf("workflow %q has no steps — define steps = [...] in %s",
			wf.Name, command.MetaFile)
	}
	if _, err := os.Stat(filepath.Join(wf.Dir, command.RunFile)); err == nil {
		return nil, fmt.Errorf("workflow %q must not have %s — workflows only compose scripts via steps",
			wf.Name, command.RunFile)
	}
	declared := map[string]bool{}
	for _, a := range wf.Args {
		declared[a.Key] = true
	}
	steps := make([]Step, 0, len(wf.Steps))
	for _, raw := range wf.Steps {
		tokens := strings.Fields(raw)
		if len(tokens) == 0 {
			return nil, fmt.Errorf("workflow %q: empty step entry", wf.Name)
		}
		name, argv := tokens[0], tokens[1:]
		for _, tok := range argv {
			for _, m := range placeholderRe.FindAllStringSubmatch(tok, -1) {
				if !declared[m[1]] {
					return nil, fmt.Errorf("workflow %q: step %q references ${%s}, which is not declared in [[args]]",
						wf.Name, raw, m[1])
				}
			}
		}
		c, err := st.Get(name)
		if err != nil {
			return nil, fmt.Errorf("workflow %q: step %q: %w", wf.Name, name, err)
		}
		if c.Kind == command.KindWorkflow {
			return nil, fmt.Errorf("workflow %q: step %q is a workflow — nesting is not supported",
				wf.Name, name)
		}
		if err := c.Runnable(); err != nil {
			return nil, fmt.Errorf("workflow %q: step %q: %w", wf.Name, name, err)
		}
		steps = append(steps, Step{Command: c, argv: argv})
	}
	return steps, nil
}

// expand substitutes ${key} placeholders in argv with the given values.
// A whole-token placeholder keeps a value with spaces as one argument.
func expand(argv []string, vals map[string]string) []string {
	out := make([]string, len(argv))
	for i, tok := range argv {
		out[i] = placeholderRe.ReplaceAllStringFunc(tok, func(m string) string {
			return vals[placeholderRe.FindStringSubmatch(m)[1]]
		})
	}
	return out
}

// Options configures one workflow run beyond the workflow itself.
type Options struct {
	// Workdir is the working directory every step runs in.
	Workdir string
	// Values are the workflow's own argument values in [[args]]
	// declaration order (the same convention as scripts); steps receive
	// them through ${key} placeholders.
	Values []string
	// Progress receives the per-step announcement ("[2/4] lint").
	Progress io.Writer
	// OnStep, when non-nil, is called right before each step starts
	// (1-based current, total, step name) so callers can mirror the
	// progress elsewhere.
	OnStep func(current, total int, name string)
	// Exec wires up each step's process: where its output goes and
	// whether it gets a process group of its own. The zero value attaches
	// the steps to exq's own terminal, which is what a synchronous run
	// wants; a background job points them at its log file instead.
	Exec runner.Options
}

// Run executes wf's steps sequentially, announcing each one on
// opts.Progress. The first failure stops execution and the remaining
// steps are recorded as skipped. Cancelling ctx terminates the step in
// flight and skips the rest. Pre-flight validation failures are returned
// as an error before any step runs; a failing step is not an error — it
// is reported in the Result.
func Run(ctx context.Context, st *store.Store, wf command.Command, opts Options) (*Result, error) {
	steps, err := Resolve(st, wf)
	if err != nil {
		return nil, err
	}
	if len(opts.Values) > len(wf.Args) {
		return nil, fmt.Errorf("workflow %q accepts at most %d argument(s), got %d",
			wf.Name, len(wf.Args), len(opts.Values))
	}
	vals := map[string]string{}
	for i, a := range wf.Args {
		v := ""
		if i < len(opts.Values) {
			v = opts.Values[i]
		}
		vals[a.Key] = v
	}
	res := &Result{Steps: make([]StepResult, 0, len(steps))}
	failed := false
	for i, s := range steps {
		if failed {
			res.Steps = append(res.Steps, StepResult{Name: s.Command.Name, Status: StatusSkipped})
			continue
		}
		fmt.Fprintf(opts.Progress, "[%d/%d] %s\n", i+1, len(steps), s.Command.Name)
		if opts.OnStep != nil {
			opts.OnStep(i+1, len(steps), s.Command.Name)
		}
		start := time.Now()
		code, runErr := runner.RunWith(ctx, s.Command, opts.Workdir, expand(s.argv, vals), opts.Exec)
		sr := StepResult{Name: s.Command.Name, ExitCode: code, Duration: time.Since(start)}
		switch {
		case runErr != nil:
			sr.Status, sr.Err, failed = StatusFailed, runErr, true
		case code != 0:
			sr.Status, failed = StatusFailed, true
		default:
			sr.Status = StatusSuccess
		}
		res.Steps = append(res.Steps, sr)
		// A cancelled run has nothing left to say: the remaining steps
		// are skipped rather than started and killed one by one.
		if ctx.Err() != nil {
			failed = true
		}
	}
	return res, nil
}

// Report writes the post-run summary of wf to out and returns the exit
// code the run should end with. The CLI and exqd share it so a workflow
// reads the same whether it ran in a terminal or into a job log.
func Report(out io.Writer, wf command.Command, res *Result) int {
	fmt.Fprintln(out)
	fmt.Fprint(out, Summary(res))
	fmt.Fprintln(out)
	if f := res.Failed(); f != nil {
		fmt.Fprintf(out, "workflow %s failed at step %s\n", wf.Name, f.Name)
		// A step that never started has no exit code of its own, so the
		// workflow still has to fail with something non-zero.
		return max(f.ExitCode, 1)
	}
	fmt.Fprintf(out, "workflow %s: all %d steps succeeded\n", wf.Name, len(res.Steps))
	return 0
}

// Summary renders the per-step outcome table shown after a run:
//
//	✓ fmt   0.3s
//	✗ test  0.4s (exit 1)
//	- build (skipped)
func Summary(res *Result) string {
	width := 0
	for _, s := range res.Steps {
		width = max(width, len(s.Name))
	}
	var b strings.Builder
	for _, s := range res.Steps {
		switch s.Status {
		case StatusSuccess:
			fmt.Fprintf(&b, "✓ %-*s %.1fs\n", width, s.Name, s.Duration.Seconds())
		case StatusFailed:
			detail := fmt.Sprintf("exit %d", s.ExitCode)
			if s.Err != nil {
				detail = s.Err.Error()
			}
			fmt.Fprintf(&b, "✗ %-*s %.1fs (%s)\n", width, s.Name, s.Duration.Seconds(), detail)
		case StatusSkipped:
			fmt.Fprintf(&b, "- %-*s (skipped)\n", width, s.Name)
		}
	}
	return b.String()
}
