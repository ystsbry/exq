package tui

import (
	"time"

	"github.com/ystsbry/exq/internal/command"
	"github.com/ystsbry/exq/internal/daemon"
	"github.com/ystsbry/exq/internal/store"
)

// sampleJobs is the fixture the jobs states render from, so the snapshot
// does not need a daemon (or any real job history) to show them.
func sampleJobs() []daemon.JobInfo {
	// Relative to now, so the snapshot reads like a real session rather
	// than a job that has been running since a fixed date.
	base := time.Now()
	return []daemon.JobInfo{
		{
			ID: "20260829-091500-a1b2", State: daemon.JobRunning,
			Spec:      daemon.JobSpec{Name: "deploy-local"},
			CreatedAt: base.Add(-42 * time.Second), StartedAt: base.Add(-42 * time.Second),
		},
		{
			ID: "20260829-090000-9f3c", State: daemon.JobSucceeded,
			Spec:      daemon.JobSpec{Name: "morning-setup"},
			CreatedAt: base.Add(-15 * time.Minute), StartedAt: base.Add(-15 * time.Minute),
			FinishedAt: base.Add(-14 * time.Minute),
		},
		{
			ID: "20260829-084500-77de", State: daemon.JobFailed, ExitCode: 1,
			Spec:      daemon.JobSpec{Name: "failing-flow"},
			CreatedAt: base.Add(-30 * time.Minute), StartedAt: base.Add(-30 * time.Minute),
			FinishedAt: base.Add(-30*time.Minute + 2*time.Second),
		},
		{
			ID: "20260829-083000-0c41", State: daemon.JobSkipped,
			Spec:      daemon.JobSpec{Name: "reset-db", ScheduleID: "demo-reset-db"},
			Reason:    "skipped: schedule demo-reset-db is still running as job 20260829-082500-5ab0",
			CreatedAt: base.Add(-45 * time.Minute),
		},
	}
}

// Snapshot is a named, pre-rendered view of one TUI state. It exists so
// `exq demo --snapshot` can print every state storybook-style without a
// TTY or key input.
type Snapshot struct {
	Name string
	View string
}

// Snapshots renders each distinct UI state with the given items. The
// empty state is always rendered from a nil list; when items itself is
// empty, built-in fixtures stand in for the item-dependent states so
// every state still renders. The args-form state uses the first command
// that declares [[args]], falling back to a built-in argful fixture.
func Snapshots(st *store.Store, items []command.Command) []Snapshot {
	if len(items) == 0 {
		items = []command.Command{
			{Name: "sample-command", Description: "snapshot 用の組み込みサンプル"},
		}
	}

	formItems := items
	formIdx := -1
	for i, it := range items {
		if len(it.Args) > 0 {
			formIdx = i
			break
		}
	}
	if formIdx < 0 {
		formItems = []command.Command{{
			Name:        "sample-args",
			Description: "引数フォームの snapshot 用サンプル",
			Args: []command.Arg{
				{Key: "env", Description: "デプロイ先環境 (dev / prod)"},
				{Key: "service", Description: "対象サービス名（空なら全サービス）"},
			},
		}}
		formIdx = 0
	}
	formBase := newModel(st, formItems)
	formBase.formIdx = formIdx
	formModel, _ := formBase.enterArgsForm()

	workflowsTab := newModel(st, items)
	workflowsTab.active = 1

	jobsTab := newModel(st, items)
	jobsTab.jobs = sampleJobs()
	jobsTab.active = jobsTab.jobsTab()

	jobsEmpty := newModel(st, items)
	jobsEmpty.active = jobsEmpty.jobsTab()
	jobsEmpty.jobsErr = "exqd is not reachable"

	jobLog := jobsTab
	jobLog = jobLog.openJobLog(jobsTab.jobs[1])
	jobLog.logLine = []string{
		"[1/2] reset-db",
		"[demo] reset-db: pretending to reset the database...",
		"[2/2] tail-logs",
		"[demo] tail-logs: pretending to tail logs...",
		"",
		"✓ reset-db   0.3s",
		"✓ tail-logs  0.2s",
		"",
		"workflow morning-setup: all 2 steps succeeded",
	}

	confirm := newModel(st, items)
	confirm.mode = modeConfirmDelete

	withErr := newModel(st, items)
	withErr.errMsg = "remove sample-command: permission denied"

	return []Snapshot{
		{Name: "browse", View: newModel(st, items).View()},
		{Name: "browse-workflows", View: workflowsTab.View()},
		{Name: "browse-jobs", View: jobsTab.View()},
		{Name: "browse-jobs-unavailable", View: jobsEmpty.View()},
		{Name: "job-log", View: jobLog.View()},
		{Name: "browse-empty", View: newModel(st, nil).View()},
		{Name: "confirm-delete", View: confirm.View()},
		{Name: "args-form", View: formModel.View()},
		{Name: "error", View: withErr.View()},
	}
}
