package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ystsbry/exq/internal/daemon"
)

// logTailLines is how much of a job's log the viewer keeps. A job that
// printed a hundred thousand lines is not worth holding in memory to
// show the last screenful of.
const logTailLines = 2000

// refreshJobs reloads the job list from the daemon. A daemon that is not
// there is not an error the user has to dismiss — the tab explains it
// and everything else keeps working.
func (m model) refreshJobs() model {
	if m.deps.Jobs == nil {
		m.jobs, m.jobsErr = nil, "exqd is not configured"
		return m
	}
	jobs, err := m.deps.Jobs.List()
	if err != nil {
		m.jobs, m.jobsErr = nil, err.Error()
		return m
	}
	m.jobs, m.jobsErr = jobs, ""
	return m.clampCursors()
}

// submitBackground hands items[idx] to the daemon and switches to the
// jobs tab so the result is immediately visible. values may be nil for a
// command without declared arguments.
func (m model) submitBackground(idx int, values []string) model {
	c := m.items[idx]
	if m.deps.Jobs == nil {
		m.errMsg = "exqd is not reachable — run `exq daemon install`"
		return m
	}
	info, err := m.deps.Jobs.Submit(daemon.JobSpec{
		ProjectDir: m.store.Root,
		Workdir:    m.store.Root,
		Name:       c.Name,
		Args:       values,
	})
	if err != nil {
		m.errMsg = backgroundError(err)
		return m
	}
	if info.State == daemon.JobSkipped {
		m.status = fmt.Sprintf("%s not started: %s", c.Name, info.Reason)
	} else {
		m.status = fmt.Sprintf("submitted %s as job %s", c.Name, info.ID)
	}
	m.active = m.jobsTab()
	m = m.refreshJobs()
	m.cursors[m.active] = 0
	m.offsets[m.active] = 0
	return m.adjustScroll()
}

// backgroundError explains a failed submit in terms of what to do next.
// exqd is installed separately from exq, so "not reachable" on its own
// leaves the user with nowhere to go.
func backgroundError(err error) string {
	if errors.Is(err, daemon.ErrUnreachable) {
		return "exqd is not reachable — run `exq daemon install`"
	}
	if errors.Is(err, daemon.ErrVersionMismatch) {
		return "exqd is a different version — run `exq daemon restart`"
	}
	return err.Error()
}

// jobsTab is the index of the jobs tab.
func (m model) jobsTab() int {
	for i, t := range m.tabs {
		if t.content == contentJobs {
			return i
		}
	}
	return m.active
}

// currentJob returns the job under the jobs tab's cursor.
func (m model) currentJob() (daemon.JobInfo, bool) {
	cur := m.cursors[m.active]
	if cur < 0 || cur >= len(m.jobs) {
		return daemon.JobInfo{}, false
	}
	return m.jobs[cur], true
}

func (m model) updateJobsTab(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		m.chosen = -1
		return m, tea.Quit
	case "left":
		m.active = (m.active - 1 + len(m.tabs)) % len(m.tabs)
	case "right":
		m.active = (m.active + 1) % len(m.tabs)
	case "up", "k":
		if m.cursors[m.active] > 0 {
			m.cursors[m.active]--
		}
	case "down", "j":
		if m.cursors[m.active] < m.rowCount()-1 {
			m.cursors[m.active]++
		}
	case "g":
		m.cursors[m.active] = 0
	case "G":
		if n := m.rowCount(); n > 0 {
			m.cursors[m.active] = n - 1
		}
	case "r":
		m.status = ""
		return m.refreshJobs().adjustScroll(), nil
	case "enter":
		if job, ok := m.currentJob(); ok {
			return m.openJobLog(job), nil
		}
	}
	return m.adjustScroll(), nil
}

// openJobLog loads a job's log for viewing. Reading the file directly is
// deliberate: the log outlives the daemon, so it can be read even when
// exqd is gone.
func (m model) openJobLog(job daemon.JobInfo) model {
	m.mode = modeJobLog
	m.logJob = job.ID
	m.logOff = 0
	data, err := os.ReadFile(daemon.JobLogPath(job.ID))
	switch {
	case err != nil:
		m.logLine = []string{"(no output)"}
	default:
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		if len(lines) > logTailLines {
			lines = append([]string{fmt.Sprintf("… %d earlier lines not shown", len(lines)-logTailLines)},
				lines[len(lines)-logTailLines:]...)
		}
		m.logLine = lines
	}
	return m
}

func (m model) updateJobLog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.chosen = -1
		return m, tea.Quit
	case "esc", "q":
		m.mode = modeBrowse
		m.logJob, m.logLine, m.logOff = "", nil, 0
	case "up", "k":
		if m.logOff > 0 {
			m.logOff--
		}
	case "down", "j":
		if m.logOff < len(m.logLine)-1 {
			m.logOff++
		}
	case "g":
		m.logOff = 0
	case "G":
		m.logOff = max(len(m.logLine)-m.logBudget(), 0)
	case "r":
		for _, job := range m.jobs {
			if job.ID == m.logJob {
				return m.openJobLog(job), nil
			}
		}
	}
	return m, nil
}

// viewJobs renders the jobs tab: one row per job, newest first.
func (m model) viewJobs() string {
	var b strings.Builder
	b.WriteString(m.viewTabBar())
	b.WriteString("\n")

	switch {
	case m.jobsErr != "":
		b.WriteString(warnStyle.Render("  " + m.jobsErr))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  set it up with `exq daemon install`"))
		b.WriteString("\n")
	case len(m.jobs) == 0:
		b.WriteString(dimStyle.Render("  no background jobs yet — press b on a script or workflow"))
		b.WriteString("\n")
	default:
		b.WriteString(m.viewJobRows())
	}
	b.WriteString("\n")

	if m.errMsg != "" {
		b.WriteString(warnStyle.Render("error: " + m.errMsg))
		b.WriteString("\n")
	}
	if m.status != "" {
		b.WriteString(dimStyle.Render(m.status))
		b.WriteString("\n")
	}
	b.WriteString(helpStyle.Render("←/→: switch tab   ↑/↓ or j/k: move   enter: log   r: refresh   q/esc: quit"))
	b.WriteString("\n")
	return b.String()
}

// viewJobRows renders the visible slice of the job list, one line per
// job plus the ↑/↓ markers when it does not all fit.
func (m model) viewJobRows() string {
	var b strings.Builder
	budget := max(m.listBudget()-2, 1)
	off := min(m.offsets[m.active], max(len(m.jobs)-1, 0))
	end := min(off+budget, len(m.jobs))
	if off > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d more", off)))
		b.WriteString("\n")
	}
	width := 0
	for _, job := range m.jobs {
		width = max(width, len(job.Spec.Name))
	}
	for i := off; i < end; i++ {
		job := m.jobs[i]
		line := fmt.Sprintf("%s  %-9s  %-*s  %s",
			job.ID, job.State, width, job.Spec.Name, jobTiming(job, time.Now()))
		if i == m.cursors[m.active] {
			b.WriteString(selCardNameStyle.Render("▸ " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}
	if end < len(m.jobs) {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↓ %d more", len(m.jobs)-end)))
		b.WriteString("\n")
	}
	return b.String()
}

// jobTiming is the start time and duration of a job — so far, for one
// still running. A job that never started has neither: a skipped run has
// nothing to show, a queued one is about to.
func jobTiming(job daemon.JobInfo, now time.Time) string {
	if job.StartedAt.IsZero() {
		if job.State.Done() {
			return "-"
		}
		return "queued"
	}
	end := job.FinishedAt
	if end.IsZero() {
		end = now
	}
	d := max(end.Sub(job.StartedAt), 0)
	return fmt.Sprintf("%s  %s", job.StartedAt.Local().Format("15:04:05"), d.Round(time.Second))
}

// logBudget is how many log lines fit on screen.
func (m model) logBudget() int {
	if m.height <= 0 {
		return len(m.logLine)
	}
	// Title, blank line, footer and the line the shell prompt returns to.
	return max(m.height-4, 1)
}

func (m model) viewJobLog() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("log: " + m.logJob))
	b.WriteString("\n\n")

	budget := m.logBudget()
	off := min(m.logOff, max(len(m.logLine)-1, 0))
	end := min(off+budget, len(m.logLine))
	for _, line := range m.logLine[off:end] {
		b.WriteString(line)
		b.WriteString("\n")
	}
	if end < len(m.logLine) {
		b.WriteString(dimStyle.Render(fmt.Sprintf("↓ %d more lines", len(m.logLine)-end)))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/↓ or j/k: scroll   g/G: top/bottom   r: reload   esc: back"))
	b.WriteString("\n")
	return b.String()
}
