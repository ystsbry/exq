package tui

import (
	"github.com/ystsbry/exq/internal/command"
	"github.com/ystsbry/exq/internal/store"
)

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

	confirm := newModel(st, items)
	confirm.mode = modeConfirmDelete

	withErr := newModel(st, items)
	withErr.errMsg = "remove sample-command: permission denied"

	return []Snapshot{
		{Name: "browse", View: newModel(st, items).View()},
		{Name: "browse-workflows", View: workflowsTab.View()},
		{Name: "browse-empty", View: newModel(st, nil).View()},
		{Name: "confirm-delete", View: confirm.View()},
		{Name: "args-form", View: formModel.View()},
		{Name: "error", View: withErr.View()},
	}
}
