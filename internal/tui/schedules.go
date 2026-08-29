package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ystsbry/exq/internal/command"
	"github.com/ystsbry/exq/internal/schedule"
)

// calendarPlaceholder shows the shape of the expression the form wants,
// which is far more useful than an empty box for a syntax most people
// meet here for the first time.
const calendarPlaceholder = "Mon..Fri 09:00"

// refreshSchedules reloads the registered schedules. Like the job list,
// an unavailable service is reported in its own tab rather than as an
// error that blocks the rest of the browser.
func (m model) refreshSchedules() model {
	if m.deps.Schedules == nil {
		m.schedules, m.schedErr = nil, "systemd schedules are not available here"
		return m
	}
	schedules, err := m.deps.Schedules.List()
	if err != nil {
		m.schedules, m.schedErr = nil, err.Error()
		return m
	}
	m.schedules, m.schedErr = schedules, ""
	return m.clampCursors()
}

// enterScheduleForm opens the registration form for items[formIdx]: the
// OnCalendar expression first, then the command's declared arguments in
// the same shape the run form uses, so filling one teaches the other.
func (m model) enterScheduleForm() (tea.Model, tea.Cmd) {
	args := m.items[m.formIdx].Args
	m.inputs = make([]textinput.Model, len(args)+1)
	for i := range m.inputs {
		ti := textinput.New()
		ti.Prompt = ""
		if i == 0 {
			ti.Placeholder = calendarPlaceholder
			ti.Focus()
		}
		m.inputs[i] = ti
	}
	m.focus = 0
	m.formErr = ""
	m.mode = modeScheduleForm
	return m, textinput.Blink
}

func (m model) updateScheduleForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.chosen = -1
		return m, tea.Quit
	case "esc":
		m.mode = modeBrowse
		m.inputs, m.formIdx, m.formErr = nil, -1, ""
		return m, nil
	case "enter":
		return m.submitSchedule(), nil
	case "tab", "down":
		return m.focusInput(m.focus + 1), nil
	case "shift+tab", "up":
		return m.focusInput(m.focus - 1), nil
	}
	var cmd tea.Cmd
	m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
	return m, cmd
}

// submitSchedule registers the schedule the form describes. A rejected
// OnCalendar expression keeps the form open with the reason attached, so
// the user can fix the expression instead of retyping everything.
func (m model) submitSchedule() model {
	if m.deps.Schedules == nil {
		m.formErr = "systemd schedules are not available here"
		return m
	}
	values := m.formValues()
	c := m.items[m.formIdx]
	s, err := m.deps.Schedules.Add(schedule.Spec{
		Name:       c.Name,
		ProjectDir: m.store.Root,
		Workdir:    m.store.Root,
		OnCalendar: strings.TrimSpace(values[0]),
		Values:     values[1:],
	})
	if err != nil {
		m.formErr = err.Error()
		return m
	}
	m.mode = modeBrowse
	m.inputs, m.formIdx, m.formErr = nil, -1, ""
	m.status = fmt.Sprintf("scheduled %s as %s (%s)", s.Name, s.ID, s.OnCalendar)
	m.active = m.schedulesTab()
	m = m.refreshSchedules()
	m.cursors[m.active] = 0
	m.offsets[m.active] = 0
	return m.adjustScroll()
}

func (m model) viewScheduleForm() string {
	it := m.items[m.formIdx]
	var b strings.Builder
	b.WriteString(titleStyle.Render("schedule: " + it.Name))
	b.WriteString("\n\n")

	labels := append([]string{"on-calendar"}, argKeys(it)...)
	width := 0
	for _, l := range labels {
		width = max(width, len(l))
	}
	for i, label := range labels {
		cursor := "  "
		key := fmt.Sprintf("%-*s", width, label)
		if i == m.focus {
			cursor = cursorStyle.Render("▸ ")
			key = keyStyle.Render(key)
		}
		fmt.Fprintf(&b, "%s%s  %s\n", cursor, key, m.inputs[i].View())
		if desc := labelHint(it, i); desc != "" {
			b.WriteString(dimStyle.Render(strings.Repeat(" ", width+4) + desc))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	if m.formErr != "" {
		b.WriteString(warnStyle.Render(m.formErr))
		b.WriteString("\n\n")
	}
	b.WriteString(helpStyle.Render("tab/↑↓: move   enter: register   esc: back"))
	b.WriteString("\n")
	return b.String()
}

// argKeys is the declared argument keys of a command, in order.
func argKeys(c command.Command) []string {
	keys := make([]string, len(c.Args))
	for i, a := range c.Args {
		keys[i] = a.Key
	}
	return keys
}

// labelHint is the help line under one schedule-form field: systemd's
// calendar syntax for the first, the argument's own description after.
func labelHint(c command.Command, i int) string {
	if i == 0 {
		return "systemd OnCalendar syntax — e.g. daily, hourly, Mon..Fri 09:00"
	}
	return c.Args[i-1].Description
}

// schedulesTab is the index of the schedules tab.
func (m model) schedulesTab() int {
	for i, t := range m.tabs {
		if t.content == contentSchedules {
			return i
		}
	}
	return m.active
}

// currentSchedule returns the schedule under the schedules tab's cursor.
func (m model) currentSchedule() (schedule.Schedule, bool) {
	cur := m.cursors[m.active]
	if cur < 0 || cur >= len(m.schedules) {
		return schedule.Schedule{}, false
	}
	return m.schedules[cur], true
}

func (m model) updateSchedulesTab(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		return m.refreshSchedules().adjustScroll(), nil
	case "enter":
		if s, ok := m.currentSchedule(); ok {
			return m.openScheduleDetail(s), nil
		}
	case "d":
		if _, ok := m.currentSchedule(); ok {
			m.mode = modeConfirmDelete
		}
	}
	return m.adjustScroll(), nil
}

// updateConfirmRemoveSchedule answers the delete prompt on the schedules
// tab, which removes units rather than a command directory.
func (m model) updateConfirmRemoveSchedule(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if s, ok := m.currentSchedule(); ok {
			if m.deps.Schedules == nil {
				m.errMsg = "systemd schedules are not available here"
			} else if err := m.deps.Schedules.Remove(s.ID); err != nil {
				m.errMsg = err.Error()
			} else {
				m.status = "removed schedule " + s.ID
				m = m.refreshSchedules().adjustScroll()
			}
		}
		m.mode = modeBrowse
	case "n", "N", "esc", "q", "ctrl+c":
		m.mode = modeBrowse
	}
	return m, nil
}

// openScheduleDetail assembles what there is to know about a schedule:
// the units exq generated and the jobs those units produced.
func (m model) openScheduleDetail(s schedule.Schedule) model {
	m.mode = modeScheduleDetail
	m.detail = s.ID
	lines := []string{
		"command:     " + s.Name,
		"project:     " + s.ProjectDir,
		"on-calendar: " + s.OnCalendar,
		"next run:    " + scheduleNext(s),
		"last submit: " + scheduleLast(s),
	}
	if len(s.Values) > 0 {
		lines = append(lines, "values:      "+strings.Join(s.Values, ", "))
	}
	if s.WorkdirMissing {
		lines = append(lines, "", warnStyle.Render("the working directory is gone — this schedule keeps failing"))
	}

	lines = append(lines, "", titleStyle.Render("jobs from this schedule"))
	if rows := m.scheduleJobs(s.ID); len(rows) == 0 {
		lines = append(lines, dimStyle.Render("  none yet"))
	} else {
		lines = append(lines, rows...)
	}

	lines = append(lines, "", titleStyle.Render(s.ServiceUnit()))
	lines = append(lines, m.unitLines(s.ServiceUnit())...)
	lines = append(lines, "", titleStyle.Render(s.TimerUnit()))
	lines = append(lines, m.unitLines(s.TimerUnit())...)

	m.detailBox = lines
	m.logOff = 0
	return m
}

// scheduleJobs are the recorded runs of one schedule, newest first.
func (m model) scheduleJobs(id string) []string {
	var rows []string
	for _, job := range m.jobs {
		if job.Spec.ScheduleID != id {
			continue
		}
		rows = append(rows, fmt.Sprintf("  %s  %-9s  %s",
			job.ID, job.State, jobTiming(job, time.Now())))
	}
	return rows
}

// unitLines is a unit file's content, or the reason it cannot be shown.
func (m model) unitLines(name string) []string {
	if m.deps.Schedules == nil {
		return []string{dimStyle.Render("  (unavailable)")}
	}
	content, err := m.deps.Schedules.ReadUnit(name)
	if err != nil {
		return []string{warnStyle.Render("  " + err.Error())}
	}
	var lines []string
	for line := range strings.SplitSeq(strings.TrimRight(content, "\n"), "\n") {
		lines = append(lines, "  "+line)
	}
	return lines
}

func (m model) updateScheduleDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.chosen = -1
		return m, tea.Quit
	case "esc", "q":
		m.mode = modeBrowse
		m.detail, m.detailBox, m.logOff = "", nil, 0
	case "up", "k":
		if m.logOff > 0 {
			m.logOff--
		}
	case "down", "j":
		if m.logOff < len(m.detailBox)-1 {
			m.logOff++
		}
	case "g":
		m.logOff = 0
	case "G":
		m.logOff = max(len(m.detailBox)-m.viewBudget(len(m.detailBox)), 0)
	}
	return m, nil
}

func (m model) viewScheduleDetail() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("schedule: " + m.detail))
	b.WriteString("\n\n")

	budget := m.viewBudget(len(m.detailBox))
	off := min(m.logOff, max(len(m.detailBox)-1, 0))
	end := min(off+budget, len(m.detailBox))
	for _, line := range m.detailBox[off:end] {
		b.WriteString(line)
		b.WriteString("\n")
	}
	if end < len(m.detailBox) {
		b.WriteString(dimStyle.Render(fmt.Sprintf("↓ %d more lines", len(m.detailBox)-end)))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/↓ or j/k: scroll   g/G: top/bottom   esc: back"))
	b.WriteString("\n")
	return b.String()
}

// viewSchedules renders the schedules tab.
func (m model) viewSchedules() string {
	var b strings.Builder
	b.WriteString(m.viewTabBar())
	b.WriteString("\n")

	switch {
	case m.schedErr != "":
		b.WriteString(warnStyle.Render("  " + m.schedErr))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  schedules need a systemd user session — see `exq daemon install`"))
		b.WriteString("\n")
	case len(m.schedules) == 0:
		b.WriteString(dimStyle.Render("  no schedules yet — press s on a script or workflow"))
		b.WriteString("\n")
	default:
		b.WriteString(m.viewScheduleRows())
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
	if m.mode == modeConfirmDelete {
		if s, ok := m.currentSchedule(); ok {
			b.WriteString(warnStyle.Render(fmt.Sprintf("remove schedule %q? [y/N]", s.ID)))
			b.WriteString("\n")
			return b.String()
		}
	}
	b.WriteString(helpStyle.Render("←/→: tab   ↑/↓: move   enter: detail   d: remove   r: refresh   q: quit"))
	b.WriteString("\n")
	return b.String()
}

// viewScheduleRows renders the visible slice of the schedule list.
func (m model) viewScheduleRows() string {
	var b strings.Builder
	budget := max(m.listBudget()-2, 1)
	off := min(m.offsets[m.active], max(len(m.schedules)-1, 0))
	end := min(off+budget, len(m.schedules))
	if off > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d more", off)))
		b.WriteString("\n")
	}
	idW, nameW, calW := 0, 0, 0
	for _, s := range m.schedules {
		idW = max(idW, len(s.ID))
		nameW = max(nameW, len(s.Name))
		calW = max(calW, len(s.OnCalendar))
	}
	for i := off; i < end; i++ {
		s := m.schedules[i]
		mark := " "
		if s.WorkdirMissing {
			mark = "!"
		}
		line := fmt.Sprintf("%s %-*s  %-*s  %-*s  %s",
			mark, idW, s.ID, nameW, s.Name, calW, s.OnCalendar, scheduleNext(s))
		if i == m.cursors[m.active] {
			b.WriteString(selCardNameStyle.Render("▸ " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}
	if end < len(m.schedules) {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↓ %d more", len(m.schedules)-end)))
		b.WriteString("\n")
	}
	return b.String()
}

// scheduleNext is when the timer fires next, as systemd sees it.
func scheduleNext(s schedule.Schedule) string {
	if s.NextElapse.IsZero() {
		return "-"
	}
	return s.NextElapse.Local().Format("2006-01-02 15:04")
}

// scheduleLast is how the most recent submit went. A oneshot that failed
// never produced a job, so this is the only trace it leaves.
func scheduleLast(s schedule.Schedule) string {
	if s.LastResult == "" {
		return "-"
	}
	return s.LastResult
}
