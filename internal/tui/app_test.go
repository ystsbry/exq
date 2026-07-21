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
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
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
	return newModel(st, items)
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

func TestEnterOnWorkflowWithoutArgsPicksImmediately(t *testing.T) {
	m := testModel(t, []command.Command{{
		Name:  "pre-pr",
		Kind:  command.KindWorkflow,
		Steps: []string{"fmt", "test"},
	}})
	// Workflows live on the second tab.
	out := step(t, m, key("right"), key("enter"))
	if out.chosen != 0 {
		t.Errorf("chosen = %d, want 0", out.chosen)
	}
	if out.mode == modeArgsForm {
		t.Error("workflow without args must not open the args form")
	}
}

func TestEnterOnWorkflowWithArgsOpensForm(t *testing.T) {
	m := testModel(t, []command.Command{{
		Name:  "install",
		Kind:  command.KindWorkflow,
		Steps: []string{"build", "install-bin ${prefix}"},
		Args:  []command.Arg{{Key: "prefix"}},
	}})
	out := step(t, m, key("right"), key("enter"))
	if out.mode != modeArgsForm {
		t.Fatalf("mode = %v, want modeArgsForm", out.mode)
	}
	out = step(t, out, key("~"), key("enter"))
	if out.chosen != 0 || len(out.values) != 1 || out.values[0] != "~" {
		t.Errorf("chosen=%d values=%q, want 0/[~]", out.chosen, out.values)
	}
}

func TestTabBarShowsBothTabsWithCounts(t *testing.T) {
	m := testModel(t, []command.Command{
		{Name: "build", Kind: command.KindScript},
		{Name: "vet", Kind: command.KindScript},
		{Name: "check", Kind: command.KindWorkflow, Steps: []string{"vet"}},
	})
	view := m.View()
	for _, want := range []string{"scripts (2)", "workflows (1)"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing tab %q:\n%s", want, view)
		}
	}
	// The scripts tab is active by default: its entries are listed, the
	// workflow's are not.
	if !strings.Contains(view, "build") || strings.Contains(view, "▸ check") {
		t.Errorf("scripts tab should list scripts only:\n%s", view)
	}
}

func TestTabSwitchFiltersAndPreservesCursor(t *testing.T) {
	m := testModel(t, []command.Command{
		{Name: "build", Kind: command.KindScript},
		{Name: "vet", Kind: command.KindScript},
		{Name: "check", Kind: command.KindWorkflow, Steps: []string{"vet"}},
	})
	// Move down inside scripts, hop to workflows and back: the scripts
	// cursor must still be on the second entry.
	out := step(t, m, key("down"), key("right"))
	if out.active != 1 {
		t.Fatalf("active = %d, want 1", out.active)
	}
	view := out.View()
	if !strings.Contains(view, "check") || strings.Contains(view, "▸ build") {
		t.Errorf("workflows tab should list workflows only:\n%s", view)
	}
	out = step(t, out, key("left"))
	if out.active != 0 || out.cursors[0] != 1 {
		t.Errorf("active=%d cursors[0]=%d, want 0/1", out.active, out.cursors[0])
	}
	// Wrap-around: left from the first tab reaches the last.
	out = step(t, out, key("left"))
	if out.active != 1 {
		t.Errorf("active = %d, want 1 (wrap)", out.active)
	}
}

func TestListViewShowsWorkflowSteps(t *testing.T) {
	m := testModel(t, []command.Command{{
		Name:        "pre-pr",
		Description: "checks",
		Kind:        command.KindWorkflow,
		Steps:       []string{"fmt", "test"},
	}})
	out := step(t, m, key("right"))
	view := out.View()
	if !strings.Contains(view, "steps: fmt → test") {
		t.Errorf("workflows tab should show step sequence:\n%s", view)
	}
}

func TestEmptyTabShowsHint(t *testing.T) {
	m := testModel(t, []command.Command{
		{Name: "build", Kind: command.KindScript},
	})
	out := step(t, m, key("right"))
	view := out.View()
	if !strings.Contains(view, "no workflows yet") {
		t.Errorf("empty workflows tab should show a hint:\n%s", view)
	}
	// Enter on an empty tab must not pick anything.
	out = step(t, out, key("enter"))
	if out.chosen != -1 {
		t.Errorf("chosen = %d, want -1 on empty tab", out.chosen)
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
