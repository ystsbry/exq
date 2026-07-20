package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ystsbry/exq/internal/command"
	"github.com/ystsbry/exq/internal/store"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func step(t *testing.T, m tea.Model, msgs ...tea.Msg) model {
	t.Helper()
	for _, msg := range msgs {
		m, _ = m.Update(msg)
	}
	return m.(model)
}

func testModel(t *testing.T, items []command.Command) model {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return model{store: st, items: items, chosen: -1}
}

func TestEnterWithoutArgsPicksImmediately(t *testing.T) {
	m := testModel(t, []command.Command{{Name: "plain"}})
	out := step(t, m, key("enter"))
	if out.chosen != 0 {
		t.Errorf("chosen = %d, want 0", out.chosen)
	}
	if out.values != nil {
		t.Errorf("values = %v, want nil", out.values)
	}
}

func TestArgsFormCollectsValuesInOrder(t *testing.T) {
	m := testModel(t, []command.Command{{
		Name: "deploy",
		Args: []command.Arg{{Key: "env"}, {Key: "service"}},
	}})

	out := step(t, m, key("enter"))
	if out.mode != modeArgsForm {
		t.Fatalf("mode = %v, want modeArgsForm", out.mode)
	}

	// Type into env, tab to service, leave it empty, then run.
	out = step(t, out, key("prod"), key("tab"), key("enter"))
	if out.chosen != 0 {
		t.Fatalf("chosen = %d, want 0", out.chosen)
	}
	if len(out.values) != 2 || out.values[0] != "prod" || out.values[1] != "" {
		t.Errorf("values = %q, want [prod \"\"]", out.values)
	}
}

func TestArgsFormEscReturnsToBrowse(t *testing.T) {
	m := testModel(t, []command.Command{{
		Name: "deploy",
		Args: []command.Arg{{Key: "env"}},
	}})
	out := step(t, m, key("enter"), key("q"), key("esc"))
	if out.mode != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse", out.mode)
	}
	if out.chosen != -1 {
		t.Errorf("chosen = %d, want -1", out.chosen)
	}
	// "q" typed inside the form must be treated as text, not quit; after esc
	// the form state is discarded.
	if out.inputs != nil {
		t.Errorf("inputs should be cleared after esc")
	}
}

func TestArgsFormViewShowsKeysAndDescriptions(t *testing.T) {
	m := testModel(t, []command.Command{{
		Name:        "deploy",
		Description: "deploy something",
		Args: []command.Arg{
			{Key: "env", Description: "target environment"},
			{Key: "service", Description: "service name"},
		},
	}})
	out := step(t, m, key("enter"))
	view := out.View()
	for _, want := range []string{"deploy", "env", "service", "target environment", "service name"} {
		if !strings.Contains(view, want) {
			t.Errorf("form view missing %q:\n%s", want, view)
		}
	}
}
