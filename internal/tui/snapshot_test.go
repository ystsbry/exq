package tui

import (
	"strings"
	"testing"

	"github.com/ystsbry/exq/internal/command"
	"github.com/ystsbry/exq/internal/store"
)

func TestSnapshotsRenderAllStates(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	items := []command.Command{
		{Name: "alpha", Description: "first sample"},
		{Name: "bravo", Description: "second sample"},
	}

	snaps := Snapshots(st, items)
	byName := map[string]string{}
	for _, s := range snaps {
		if s.View == "" {
			t.Errorf("snapshot %q rendered empty", s.Name)
		}
		byName[s.Name] = s.View
	}

	for _, want := range []string{"browse", "browse-empty", "confirm-delete", "error"} {
		if _, ok := byName[want]; !ok {
			t.Errorf("missing snapshot %q", want)
		}
	}
	if !strings.Contains(byName["browse"], "alpha") {
		t.Errorf("browse snapshot should list commands:\n%s", byName["browse"])
	}
	if !strings.Contains(byName["browse-empty"], "no commands yet") {
		t.Errorf("empty snapshot should show the empty hint:\n%s", byName["browse-empty"])
	}
	if !strings.Contains(byName["confirm-delete"], "delete") {
		t.Errorf("confirm-delete snapshot should show the prompt:\n%s", byName["confirm-delete"])
	}
	if !strings.Contains(byName["error"], "permission denied") {
		t.Errorf("error snapshot should show the error:\n%s", byName["error"])
	}
}
