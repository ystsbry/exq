// Package tui provides the interactive command browser: a top tab bar
// (scripts / workflows) with ←/→ switching, a per-tab list to pick a
// command to execute (filling its declared arguments in a form), or
// delete one. The program runs to completion and returns the command the
// user chose to run plus the argument values (nil when they just quit);
// actually executing it is left to the caller so the terminal is restored
// before the command's output starts.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ystsbry/exq/internal/command"
	"github.com/ystsbry/exq/internal/store"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	helpStyle     = lipgloss.NewStyle().Faint(true)
	warnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	keyStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))

	// Tabs render as bordered boxes; the active one gets the accent color.
	activeTabStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("205")).
			Foreground(lipgloss.Color("212")).
			Bold(true).
			Padding(0, 1)
	inactiveTabStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("240")).
				Faint(true).
				Padding(0, 1)
)

// Result is what the user chose in the TUI: the command to execute and the
// argument values collected from the form, in [[args]] declaration order.
type Result struct {
	Command command.Command
	Values  []string
}

// Run shows the tabbed command browser until the user picks a command to
// execute or quits. Commands with declared [[args]] go through an input
// form first. Returns nil when the user quit without choosing.
func Run(st *store.Store) (*Result, error) {
	items, err := st.List()
	if err != nil {
		return nil, err
	}
	m := newModel(st, items)
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return nil, err
	}
	out := final.(model)
	if out.chosen < 0 || out.chosen >= len(out.items) {
		return nil, nil
	}
	return &Result{Command: out.items[out.chosen], Values: out.values}, nil
}

type mode int

const (
	modeBrowse mode = iota
	modeConfirmDelete
	modeArgsForm
)

// tabDef is one entry in the top tab bar. Both current tabs render the
// command list filtered by kind; future tabs (logs, history, …) plug in
// by appending a tabDef here and branching to their own view/update in
// the model — the bar, ←/→ switching, and per-tab cursor bookkeeping are
// already generic over the tab list.
type tabDef struct {
	title string
	kind  command.Kind
}

type model struct {
	store   *store.Store
	items   []command.Command
	tabs    []tabDef
	active  int   // index of the active tab
	cursors []int // per-tab cursor position, preserved across switches
	mode    mode
	errMsg  string
	chosen  int      // index into items of the command to execute; -1 for none
	values  []string // argument values collected by the form
	width   int

	formIdx int               // index into items of the command whose args form is open
	inputs  []textinput.Model // one per Arg of that command
	focus   int               // focused index in inputs
}

func newModel(st *store.Store, items []command.Command) model {
	tabs := []tabDef{
		{title: "scripts", kind: command.KindScript},
		{title: "workflows", kind: command.KindWorkflow},
	}
	return model{
		store:   st,
		items:   items,
		tabs:    tabs,
		cursors: make([]int, len(tabs)),
		chosen:  -1,
		formIdx: -1,
	}
}

// tabIdxs returns the indices into items that belong to the active tab.
func (m model) tabIdxs() []int {
	var idxs []int
	for i, it := range m.items {
		if it.Kind == m.tabs[m.active].kind {
			idxs = append(idxs, i)
		}
	}
	return idxs
}

// current returns the items index under the active tab's cursor.
func (m model) current() (int, bool) {
	idxs := m.tabIdxs()
	cur := m.cursors[m.active]
	if len(idxs) == 0 || cur >= len(idxs) {
		return -1, false
	}
	return idxs[cur], true
}

// kindCount counts the items of one kind.
func (m model) kindCount(k command.Kind) int {
	n := 0
	for _, it := range m.items {
		if it.Kind == k {
			n++
		}
	}
	return n
}

// clampCursors pulls every tab cursor back into range after items changed.
func (m model) clampCursors() model {
	for ti, t := range m.tabs {
		n := m.kindCount(t.kind)
		if m.cursors[ti] >= n {
			if n == 0 {
				m.cursors[ti] = 0
			} else {
				m.cursors[ti] = n - 1
			}
		}
	}
	return m
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch m.mode {
		case modeConfirmDelete:
			return m.updateConfirmDelete(msg)
		case modeArgsForm:
			return m.updateArgsForm(msg)
		default:
			return m.updateBrowse(msg)
		}
	}
	return m, nil
}

func (m model) updateBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.errMsg = ""
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
		if m.cursors[m.active] < len(m.tabIdxs())-1 {
			m.cursors[m.active]++
		}
	case "g":
		m.cursors[m.active] = 0
	case "G":
		if n := len(m.tabIdxs()); n > 0 {
			m.cursors[m.active] = n - 1
		}
	case "enter":
		idx, ok := m.current()
		if !ok {
			break
		}
		// Commands without declared args keep the two-keystroke flow:
		// enter picks and quits immediately.
		if len(m.items[idx].Args) == 0 {
			m.chosen = idx
			return m, tea.Quit
		}
		m.formIdx = idx
		return m.enterArgsForm()
	case "d":
		if _, ok := m.current(); ok {
			m.mode = modeConfirmDelete
		}
	}
	return m, nil
}

func (m model) updateConfirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if idx, ok := m.current(); ok {
			if err := m.store.Remove(m.items[idx].Name); err != nil {
				m.errMsg = err.Error()
			} else if items, err := m.store.List(); err != nil {
				m.errMsg = err.Error()
			} else {
				m.items = items
				m = m.clampCursors()
			}
		}
		m.mode = modeBrowse
	case "n", "N", "esc", "q", "ctrl+c":
		m.mode = modeBrowse
	}
	return m, nil
}

// enterArgsForm switches to the argument form for items[formIdx], with
// one text input per declared arg and the first one focused.
func (m model) enterArgsForm() (tea.Model, tea.Cmd) {
	args := m.items[m.formIdx].Args
	m.inputs = make([]textinput.Model, len(args))
	for i := range args {
		ti := textinput.New()
		ti.Prompt = ""
		if i == 0 {
			ti.Focus()
		}
		m.inputs[i] = ti
	}
	m.focus = 0
	m.mode = modeArgsForm
	return m, textinput.Blink
}

func (m model) updateArgsForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.chosen = -1
		return m, tea.Quit
	case "esc":
		m.mode = modeBrowse
		m.inputs = nil
		m.formIdx = -1
		return m, nil
	case "enter":
		m.values = make([]string, len(m.inputs))
		for i, in := range m.inputs {
			m.values[i] = in.Value()
		}
		m.chosen = m.formIdx
		return m, tea.Quit
	case "tab", "down":
		return m.focusInput(m.focus + 1), nil
	case "shift+tab", "up":
		return m.focusInput(m.focus - 1), nil
	}
	var cmd tea.Cmd
	m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
	return m, cmd
}

// focusInput moves focus to input i, wrapping around both ends.
func (m model) focusInput(i int) model {
	n := len(m.inputs)
	i = ((i % n) + n) % n
	m.inputs[m.focus].Blur()
	m.inputs[i].Focus()
	m.focus = i
	return m
}

func (m model) View() string {
	if m.mode == modeArgsForm {
		return m.viewArgsForm()
	}
	return m.viewList()
}

// viewTabBar renders the boxed tabs, active one highlighted.
func (m model) viewTabBar() string {
	labels := make([]string, len(m.tabs))
	for i, t := range m.tabs {
		label := fmt.Sprintf("%s (%d)", t.title, m.kindCount(t.kind))
		if i == m.active {
			labels[i] = activeTabStyle.Render(label)
		} else {
			labels[i] = inactiveTabStyle.Render(label)
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Bottom, labels...)
}

// emptyTabHint tells how to add an entry of the active tab's kind.
func (m model) emptyTabHint() string {
	if m.tabs[m.active].kind == command.KindWorkflow {
		return "  no workflows yet — define steps in " + m.store.WorkflowsDir() + "/<name>/command.toml"
	}
	return "  no scripts yet — add one under " + m.store.ScriptsDir()
}

func (m model) viewList() string {
	var b strings.Builder
	b.WriteString(m.viewTabBar())
	b.WriteString("\n")

	idxs := m.tabIdxs()
	if len(idxs) == 0 {
		b.WriteString(dimStyle.Render(m.emptyTabHint()))
		b.WriteString("\n")
	}
	for pos, idx := range idxs {
		it := m.items[idx]
		cursor := "  "
		name := it.Name
		if pos == m.cursors[m.active] {
			cursor = cursorStyle.Render("▸ ")
			name = selectedStyle.Render(name)
		}
		fmt.Fprintf(&b, "%s%s\n", cursor, name)
		if meta := describeItem(it); meta != "" {
			b.WriteString(dimStyle.Render("    " + meta))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	if m.errMsg != "" {
		b.WriteString(warnStyle.Render("error: " + m.errMsg))
		b.WriteString("\n")
	}
	switch m.mode {
	case modeConfirmDelete:
		if idx, ok := m.current(); ok {
			b.WriteString(warnStyle.Render(fmt.Sprintf("delete %q? [y/N]", m.items[idx].Name)))
		}
	default:
		b.WriteString(helpStyle.Render("←/→: switch tab   ↑/↓ or j/k: move   enter: run   d: delete   q/esc: quit"))
	}
	b.WriteString("\n")
	return b.String()
}

func (m model) viewArgsForm() string {
	it := m.items[m.formIdx]
	var b strings.Builder

	head := "run: " + it.Name
	if it.Description != "" {
		head += " — " + it.Description
	}
	b.WriteString(titleStyle.Render(head))
	b.WriteString("\n\n")

	keyWidth := 0
	for _, a := range it.Args {
		if len(a.Key) > keyWidth {
			keyWidth = len(a.Key)
		}
	}
	for i, a := range it.Args {
		cursor := "  "
		key := fmt.Sprintf("%-*s", keyWidth, a.Key)
		if i == m.focus {
			cursor = cursorStyle.Render("▸ ")
			key = keyStyle.Render(key)
		}
		fmt.Fprintf(&b, "%s%s  %s\n", cursor, key, m.inputs[i].View())
		if a.Description != "" {
			b.WriteString(dimStyle.Render(strings.Repeat(" ", keyWidth+4) + a.Description))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("tab/↑↓: move   enter: run (empty = \"\")   esc: back"))
	b.WriteString("\n")
	return b.String()
}

// describeItem is the dim meta line under a command name in the list:
// the description plus the declared argument keys (scripts) or the step
// sequence (workflows), either part optional.
func describeItem(it command.Command) string {
	meta := it.Description
	if it.Kind == command.KindWorkflow && len(it.Steps) > 0 {
		meta = strings.TrimSpace(meta + " (steps: " + strings.Join(it.Steps, " → ") + ")")
	} else if len(it.Args) > 0 {
		keys := make([]string, len(it.Args))
		for i, a := range it.Args {
			keys[i] = a.Key
		}
		meta = strings.TrimSpace(meta + " (args: " + strings.Join(keys, ", ") + ")")
	}
	return meta
}
