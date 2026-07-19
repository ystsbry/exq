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

// Snapshots renders each distinct UI state with the given items. The empty
// state is always rendered from a nil list; when items itself is empty, a
// built-in fixture stands in for the item-dependent states so every state
// still renders.
func Snapshots(st *store.Store, items []command.Command) []Snapshot {
	if len(items) == 0 {
		items = []command.Command{
			{Name: "sample-command", Description: "snapshot 用の組み込みサンプル"},
		}
	}
	return []Snapshot{
		{
			Name: "browse",
			View: model{store: st, items: items, cursor: min(1, len(items)-1)}.View(),
		},
		{
			Name: "browse-empty",
			View: model{store: st}.View(),
		},
		{
			Name: "confirm-delete",
			View: model{store: st, items: items, mode: modeConfirmDelete}.View(),
		},
		{
			Name: "error",
			View: model{store: st, items: items, errMsg: "remove sample-command: permission denied"}.View(),
		},
	}
}
