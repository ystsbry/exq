package daemon

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStateDirFollowsXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	if got, want := StateDir(), filepath.Join("/xdg/state", "exq"); got != want {
		t.Fatalf("StateDir() = %q, want %q", got, want)
	}
}

func TestStateDirFallsBackToLocalState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/tester")
	if got, want := StateDir(), "/home/tester/.local/state/exq"; got != want {
		t.Fatalf("StateDir() = %q, want %q", got, want)
	}
}

func TestStateDirStaysAbsoluteWithoutAHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	if got := StateDir(); !filepath.IsAbs(got) {
		t.Fatalf("StateDir() = %q, want an absolute path", got)
	}
}

func TestJobPathsNestUnderTheStateDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	id := "20260829-010203-a1b2"
	if got, want := JobsDir(), "/xdg/state/exq/jobs"; got != want {
		t.Fatalf("JobsDir() = %q, want %q", got, want)
	}
	if got, want := JobDir(id), "/xdg/state/exq/jobs/"+id; got != want {
		t.Fatalf("JobDir() = %q, want %q", got, want)
	}
	if got := JobLogPath(id); !strings.HasSuffix(got, filepath.Join(id, LogFile)) {
		t.Fatalf("JobLogPath() = %q, want it to end in %s/%s", got, id, LogFile)
	}
}
