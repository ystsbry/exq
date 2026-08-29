package tui

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
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

func TestListScrollsToKeepCursorVisible(t *testing.T) {
	var items []command.Command
	for _, n := range []string{"a1", "b2", "c3", "d4", "e5", "f6"} {
		items = append(items, command.Command{Name: n, Description: "desc " + n})
	}
	m := testModel(t, items)
	out := step(t, m, tea.WindowSizeMsg{Width: 80, Height: 14})

	// Jump to the last entry: the view must stay within the terminal
	// height, keep the cursor visible, and show the ↑ indicator.
	out = step(t, out, key("G"))
	view := out.View()
	if h := lipgloss.Height(view); h > 14 {
		t.Errorf("view height = %d, want <= 14:\n%s", h, view)
	}
	if !strings.Contains(view, "▸ f6") {
		t.Errorf("cursor card should be visible:\n%s", view)
	}
	if !strings.Contains(view, "↑ 4 more") {
		t.Errorf("expected ↑ more indicator:\n%s", view)
	}

	// Back to the top: no ↑ indicator, hidden items announced below.
	out = step(t, out, key("g"))
	view = out.View()
	if !strings.Contains(view, "▸ a1") || strings.Contains(view, "↑ 4 more") {
		t.Errorf("top of list should show first card without ↑ indicator:\n%s", view)
	}
	if !strings.Contains(view, "↓ 4 more") {
		t.Errorf("expected ↓ more indicator:\n%s", view)
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

func TestInitIssuesNoCommand(t *testing.T) {
	m := testModel(t, []command.Command{{Name: "plain"}})
	if cmd := m.Init(); cmd != nil {
		t.Errorf("Init() = %v, want nil — the model needs no startup work", cmd)
	}
}

func TestQuitKeysChooseNothing(t *testing.T) {
	for _, k := range []string{"q", "esc", "ctrl+c"} {
		t.Run(k, func(t *testing.T) {
			m := testModel(t, []command.Command{{Name: "plain"}})
			out := step(t, m, key(k))
			if out.chosen != -1 {
				t.Errorf("chosen = %d, want -1", out.chosen)
			}
		})
	}
}

func TestBrowseCursorStaysInBounds(t *testing.T) {
	m := testModel(t, []command.Command{{Name: "a"}, {Name: "b"}})

	// Up at the top and down at the bottom are no-ops rather than wrapping.
	out := step(t, m, key("up"), key("k"))
	if out.cursors[0] != 0 {
		t.Errorf("cursor = %d, want 0 at the top of the list", out.cursors[0])
	}
	out = step(t, out, key("down"), key("j"), key("j"))
	if out.cursors[0] != 1 {
		t.Errorf("cursor = %d, want 1 at the bottom of the list", out.cursors[0])
	}
	// G on an empty tab must not move the cursor off the end.
	out = step(t, out, key("right"), key("G"))
	if out.cursors[1] != 0 {
		t.Errorf("cursor = %d, want 0 on an empty tab", out.cursors[1])
	}
}

func TestArgsFormCtrlCQuitsWithoutChoosing(t *testing.T) {
	m := testModel(t, []command.Command{{
		Name: "deploy",
		Args: []command.Arg{{Key: "env"}},
	}})
	out := step(t, m, key("enter"), key("prod"), key("ctrl+c"))
	if out.chosen != -1 {
		t.Errorf("chosen = %d, want -1", out.chosen)
	}
}

func TestArgsFormFocusWrapsBothWays(t *testing.T) {
	m := testModel(t, []command.Command{{
		Name: "deploy",
		Args: []command.Arg{{Key: "env"}, {Key: "service"}},
	}})
	out := step(t, m, key("enter"))

	// shift+tab from the first input wraps to the last, tab wraps back.
	out = step(t, out, key("shift+tab"))
	if out.focus != 1 {
		t.Errorf("focus = %d, want 1 after wrapping backwards", out.focus)
	}
	out = step(t, out, key("tab"))
	if out.focus != 0 {
		t.Errorf("focus = %d, want 0 after wrapping forwards", out.focus)
	}
	// ↑/↓ move focus too, and typing lands in the focused input only.
	out = step(t, out, key("down"), key("api"), key("enter"))
	if len(out.values) != 2 || out.values[0] != "" || out.values[1] != "api" {
		t.Errorf("values = %q, want [\"\" api]", out.values)
	}
}

// storeWithScripts creates a store whose scripts/ holds one runnable entry
// per name, and returns it together with the discovered items.
func storeWithScripts(t *testing.T, names ...string) (*store.Store, []command.Command) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		dir := filepath.Join(st.ScriptsDir(), n)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, command.RunFile), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	items, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	return st, items
}

func TestConfirmDeletePromptAndRemoval(t *testing.T) {
	// The doomed entry sorts last, so deleting it also exercises pulling the
	// cursor back into range.
	st, items := storeWithScripts(t, "keep", "zap")
	m := newModel(st, items)

	// "d" opens the confirmation, which names the command about to go.
	out := step(t, m, key("down"), key("d"))
	if out.mode != modeConfirmDelete {
		t.Fatalf("mode = %v, want modeConfirmDelete", out.mode)
	}
	if view := out.View(); !strings.Contains(view, `delete "zap"? [y/N]`) {
		t.Errorf("confirmation prompt missing:\n%s", view)
	}

	out = step(t, out, key("y"))
	if out.mode != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse after confirming", out.mode)
	}
	if out.errMsg != "" {
		t.Errorf("errMsg = %q, want empty", out.errMsg)
	}
	if len(out.items) != 1 || out.items[0].Name != "keep" {
		t.Fatalf("items = %+v, want only keep", out.items)
	}
	if _, err := os.Stat(filepath.Join(st.ScriptsDir(), "zap")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("command directory should be gone: %v", err)
	}
	// The cursor was on the deleted (last) entry, so it must come back into
	// range instead of pointing past the end.
	if out.cursors[0] != 0 {
		t.Errorf("cursor = %d, want 0 after the last entry was deleted", out.cursors[0])
	}
}

func TestConfirmDeleteCancelKeys(t *testing.T) {
	for _, k := range []string{"n", "N", "esc", "q", "ctrl+c"} {
		t.Run(k, func(t *testing.T) {
			st, items := storeWithScripts(t, "keep")
			out := step(t, newModel(st, items), key("d"), key(k))
			if out.mode != modeBrowse {
				t.Errorf("mode = %v, want modeBrowse", out.mode)
			}
			if len(out.items) != 1 {
				t.Errorf("items = %+v, want the command kept", out.items)
			}
			if _, err := os.Stat(filepath.Join(st.ScriptsDir(), "keep")); err != nil {
				t.Errorf("command directory should survive: %v", err)
			}
		})
	}
}

func TestConfirmDeleteUnknownKeyStaysInPrompt(t *testing.T) {
	st, items := storeWithScripts(t, "keep")
	out := step(t, newModel(st, items), key("d"), key("x"))
	if out.mode != modeConfirmDelete {
		t.Errorf("mode = %v, want modeConfirmDelete — an unrelated key must not decide", out.mode)
	}
}

func TestConfirmDeleteShowsRemovalError(t *testing.T) {
	st, items := storeWithScripts(t, "doomed")
	out := step(t, newModel(st, items), key("d"))

	// The directory disappears between the prompt and the confirmation.
	if err := os.RemoveAll(st.Dir()); err != nil {
		t.Fatal(err)
	}
	out = step(t, out, key("y"))

	if out.errMsg == "" {
		t.Error("errMsg is empty, want the removal failure surfaced")
	}
	if out.mode != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse", out.mode)
	}
	if view := out.View(); !strings.Contains(view, "error: ") {
		t.Errorf("view should show the error:\n%s", view)
	}
}

func TestConfirmDeleteShowsReloadError(t *testing.T) {
	st, items := storeWithScripts(t, "keep", "zap")
	out := step(t, newModel(st, items), key("down"), key("d"))

	// The removal itself succeeds, but re-reading the store afterwards does
	// not: workflows/ is now a regular file.
	if err := os.WriteFile(st.WorkflowsDir(), []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = step(t, out, key("y"))

	if out.errMsg == "" {
		t.Error("errMsg is empty, want the reload failure surfaced")
	}
	if _, err := os.Stat(filepath.Join(st.ScriptsDir(), "zap")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the command was still removed, so it should be gone: %v", err)
	}
}

func TestNarrowTerminalCapsCardWidth(t *testing.T) {
	m := testModel(t, []command.Command{{
		Name:        "a-command-with-a-very-long-name",
		Description: "and a description that is longer still, well past any sane terminal width",
	}})
	const width = 24
	out := step(t, m, tea.WindowSizeMsg{Width: width, Height: 20})

	if got := out.cardWidth(out.tabIdxs()); got > width {
		t.Errorf("cardWidth() = %d, want <= %d", got, width)
	}
	// The selected card's background block is what the cap protects: without
	// it the block would stretch to the longest description and wrap. The
	// tab bar and the help footer are fixed-width chrome and not capped.
	for _, line := range strings.Split(out.View(), "\n") {
		if !strings.Contains(line, "▸ ") {
			continue
		}
		if w := lipgloss.Width(line); w > width {
			t.Errorf("card line width = %d, want <= %d: %q", w, width, line)
		}
	}
}

func TestListBudgetNeverFallsBelowOneCard(t *testing.T) {
	items := []command.Command{{Name: "a1", Description: "with a meta line"}}
	m := testModel(t, items)
	m.errMsg = "something went wrong"

	// visibleEnd relies on a whole card always fitting in the budget. Walk
	// the terminal heights that could break that, including absurd ones.
	for _, height := range []int{1, 2, 3, 5, 8, 13, 40} {
		out := step(t, m, tea.WindowSizeMsg{Width: 80, Height: height})
		if got := out.listBudget(); got < maxBlockHeight {
			t.Errorf("listBudget() = %d at height %d, want >= %d", got, height, maxBlockHeight)
		}
		if got := out.blockHeight(items[0]); got > maxBlockHeight {
			t.Errorf("blockHeight() = %d, want <= %d", got, maxBlockHeight)
		}
		// The cursor's card is therefore always rendered.
		if end := out.visibleEnd(out.tabIdxs(), 0, out.listBudget()); end < 1 {
			t.Errorf("visibleEnd() = %d at height %d, want at least one card", end, height)
		}
	}
}

func TestShortTerminalWithErrorStaysWithinHeight(t *testing.T) {
	var items []command.Command
	for _, n := range []string{"a1", "b2", "c3", "d4", "e5"} {
		items = append(items, command.Command{Name: n, Description: "desc " + n})
	}
	m := testModel(t, items)
	m.errMsg = "remove a1: permission denied"
	out := step(t, m, tea.WindowSizeMsg{Width: 80, Height: 9})

	view := out.View()
	// The error line eats into the list budget, so fewer cards fit — but the
	// cursor's card and the error both have to stay on screen.
	if !strings.Contains(view, "▸ a1") {
		t.Errorf("cursor card should stay visible:\n%s", view)
	}
	if !strings.Contains(view, "permission denied") {
		t.Errorf("error should stay visible:\n%s", view)
	}
}

func TestDeleteOnEmptyTabIsIgnored(t *testing.T) {
	st, items := storeWithScripts(t, "only-script")
	// The workflows tab has nothing to delete, so the prompt must not open.
	out := step(t, newModel(st, items), key("right"), key("d"))
	if out.mode != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse on an empty tab", out.mode)
	}
}
