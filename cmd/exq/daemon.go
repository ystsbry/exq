package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ystsbry/exq/internal/daemon"
)

// daemonClient returns a client for the exqd socket at its conventional
// location.
func daemonClient() *daemon.Client {
	return daemon.NewClient("")
}

// daemonHint turns a transport failure into advice. exqd is a separate,
// separately installed binary, so "connection refused" on its own leaves
// the user with nothing to act on.
func daemonHint(err error) error {
	switch {
	case errors.Is(err, daemon.ErrUnreachable):
		return fmt.Errorf("%w — run `exq daemon install` to set it up, or `exq daemon status` to check it", err)
	case errors.Is(err, daemon.ErrVersionMismatch):
		return fmt.Errorf("%w — run `exq daemon restart` so exqd picks up the installed version", err)
	}
	return err
}

// submitJob hands one job to exqd and reports the outcome on out. A
// submit that exqd skipped — the previous run of the same schedule was
// still going — is reported, not failed: the timer's oneshot service
// did its job, so it has to exit cleanly.
func submitJob(out io.Writer, spec daemon.JobSpec) error {
	info, err := daemonClient().Submit(spec)
	if err != nil {
		return daemonHint(err)
	}
	if info.State == daemon.JobSkipped {
		fmt.Fprintf(out, "%s not started: %s\n", spec.Name, info.Reason)
		return nil
	}
	fmt.Fprintf(out, "submitted %s as job %s\n", spec.Name, info.ID)
	fmt.Fprintf(out, "  exq logs %s -f   # follow the output\n", info.ID)
	return nil
}

// writeJobTable renders jobs as an aligned table, newest first. now is
// what an unfinished job's duration is measured against.
func writeJobTable(out io.Writer, jobs []daemon.JobInfo, now time.Time) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "JOB\tSTATE\tCOMMAND\tSTARTED\tDURATION")
	for _, j := range jobs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			j.ID, j.State, j.Spec.Name, startedAt(j), jobDuration(j, now))
	}
	_ = tw.Flush()
}

// startedAt is the local start time of a job, or a dash while it is
// still queued.
func startedAt(j daemon.JobInfo) string {
	if j.StartedAt.IsZero() {
		return "-"
	}
	return j.StartedAt.Local().Format("2006-01-02 15:04:05")
}

// jobDuration is how long a job ran — so far, for one still running.
func jobDuration(j daemon.JobInfo, now time.Time) string {
	if j.StartedAt.IsZero() {
		return "-"
	}
	end := j.FinishedAt
	if end.IsZero() {
		end = now
	}
	d := end.Sub(j.StartedAt)
	if d < 0 {
		d = 0
	}
	return d.Round(100 * time.Millisecond).String()
}

// jobOutcome is the one-line verdict shown after a job finished: the
// state, plus whatever explains it.
func jobOutcome(j daemon.JobInfo) string {
	var b strings.Builder
	b.WriteString(string(j.State))
	if j.State == daemon.JobFailed && j.ExitCode > 0 {
		fmt.Fprintf(&b, " (exit %d)", j.ExitCode)
	}
	if j.Reason != "" {
		fmt.Fprintf(&b, ": %s", j.Reason)
	}
	return b.String()
}

// jobSpecFromWD builds the spec for a job submitted from the current
// working directory. The project directory (where .exq/ is resolved) and
// the working directory are the same for a manual submit — a schedule is
// what can eventually make them differ.
func jobSpecFromWD(name string, values []string) (daemon.JobSpec, error) {
	wd, err := os.Getwd()
	if err != nil {
		return daemon.JobSpec{}, err
	}
	return daemon.JobSpec{ProjectDir: wd, Workdir: wd, Name: name, Args: values}, nil
}
