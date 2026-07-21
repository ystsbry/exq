// Package tui provides the interactive command list: browse commands,
// pick one to execute (filling its declared arguments in a form), or
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
)

// Result is what the user chose in the TUI: the command to execute and the
// argument values collected from the form, in [[args]] declaration order.
type Result struct {
	Command command.Command
	Values  []string
}

// Run shows the command list until the user picks a command to execute or
// quits. Commands with declared [[args]] go through an input form first.
// Returns nil when the user quit without choosing.
func Run(st *store.Store) (*Result, error) {
	items, err := st.List()
	if err != nil {
		return nil, err
	}
	m := model{store: st, items: items, chosen: -1}
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

type model struct {
	store  *store.Store
	items  []command.Command
	cursor int
	mode   mode
	errMsg string
	chosen int      // index of the command to execute after quit; -1 for none
	values []string // argument values collected by the form
	width  int

	inputs []textinput.Model // one per Arg of the command under the cursor
	focus  int               // focused index in inputs
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
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "g":
		m.cursor = 0
	case "G":
		if len(m.items) > 0 {
			m.cursor = len(m.items) - 1
		}
	case "enter":
		if len(m.items) == 0 {
			break
		}
		// Commands without declared args keep the two-keystroke flow:
		// enter picks and quits immediately. Scripts and workflows share
		// the same form — workflows declare [[args]] and feed the values
		// to their steps via ${key} placeholders.
		if len(m.items[m.cursor].Args) == 0 {
			m.chosen = m.cursor
			return m, tea.Quit
		}
		return m.enterArgsForm()
	case "d":
		if len(m.items) > 0 {
			m.mode = modeConfirmDelete
		}
	}
	return m, nil
}

func (m model) updateConfirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		name := m.items[m.cursor].Name
		if err := m.store.Remove(name); err != nil {
			m.errMsg = err.Error()
		} else if items, err := m.store.List(); err != nil {
			m.errMsg = err.Error()
		} else {
			m.items = items
			if m.cursor >= len(m.items) && m.cursor > 0 {
				m.cursor = len(m.items) - 1
			}
		}
		m.mode = modeBrowse
	case "n", "N", "esc", "q", "ctrl+c":
		m.mode = modeBrowse
	}
	return m, nil
}

// enterArgsForm switches to the argument form for the command under the
// cursor, with one text input per declared arg and the first one focused.
func (m model) enterArgsForm() (tea.Model, tea.Cmd) {
	args := m.items[m.cursor].Args
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
		return m, nil
	case "enter":
		m.values = make([]string, len(m.inputs))
		for i, in := range m.inputs {
			m.values[i] = in.Value()
		}
		m.chosen = m.cursor
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

func (m model) viewList() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("exq commands (%d)", len(m.items))))
	b.WriteString("\n\n")

	if len(m.items) == 0 {
		b.WriteString(dimStyle.Render("  no commands yet — add one under " + m.store.ScriptsDir()))
		b.WriteString("\n")
	}
	for i, it := range m.items {
		cursor := "  "
		name := it.Name
		if i == m.cursor {
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
		b.WriteString(warnStyle.Render(fmt.Sprintf("delete %q? [y/N]", m.items[m.cursor].Name)))
	default:
		b.WriteString(helpStyle.Render("↑/↓ or j/k: move   enter: run   d: delete   q/esc: quit"))
	}
	b.WriteString("\n")
	return b.String()
}

func (m model) viewArgsForm() string {
	it := m.items[m.cursor]
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
