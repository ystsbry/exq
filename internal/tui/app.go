// Package tui provides the interactive command list: browse commands,
// pick one to execute, or delete one. The program runs to completion and
// returns the command the user chose to run (nil when they just quit);
// actually executing it is left to the caller so the terminal is restored
// before the command's output starts.
package tui

import (
	"fmt"
	"strings"

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
)

// Run shows the command list until the user picks a command to execute or
// quits. Returns nil when the user quit without choosing.
func Run(st *store.Store) (*command.Command, error) {
	items, err := st.List()
	if err != nil {
		return nil, err
	}
	m := model{store: st, items: items}
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return nil, err
	}
	out := final.(model)
	if out.chosen < 0 || out.chosen >= len(out.items) {
		return nil, nil
	}
	pick := out.items[out.chosen]
	return &pick, nil
}

type mode int

const (
	modeBrowse mode = iota
	modeConfirmDelete
)

type model struct {
	store  *store.Store
	items  []command.Command
	cursor int
	mode   mode
	errMsg string
	chosen int // index of the command to execute after quit; -1 for none
	width  int
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		if m.mode == modeConfirmDelete {
			return m.updateConfirmDelete(msg)
		}
		return m.updateBrowse(msg)
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
		if len(m.items) > 0 {
			m.chosen = m.cursor
			return m, tea.Quit
		}
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

func (m model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("exq commands (%d)", len(m.items))))
	b.WriteString("\n\n")

	if len(m.items) == 0 {
		b.WriteString(dimStyle.Render("  no commands yet — add one under " + m.store.CommandsDir()))
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
		if it.Description != "" {
			b.WriteString(dimStyle.Render("    " + it.Description))
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
