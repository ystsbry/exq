package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ystsbry/exq/internal/daemon"
)

// devNull is a stand-in for the process streams in tests that do not care
// about the output.
func devNull(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// capture runs fn with a temporary file standing in for a process stream
// and returns what was written to it.
func capture(t *testing.T, fn func(f *os.File)) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	fn(f)
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestVersionFlagPrintsTheProtocolVersion(t *testing.T) {
	out := capture(t, func(f *os.File) {
		if err := run([]string{"-version"}, f, devNull(t)); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "exqd") || !strings.Contains(out, "protocol v") {
		t.Fatalf("version output = %q", out)
	}
}

func TestUnknownFlagIsReported(t *testing.T) {
	if err := run([]string{"-nope"}, devNull(t), devNull(t)); err == nil {
		t.Fatal("want an error for an unknown flag")
	}
}

func TestServeAnswersUntilSignalled(t *testing.T) {
	// Unix socket paths are capped around 108 bytes, so keep it short.
	dir, err := os.MkdirTemp("", "exqd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "exqd.sock")
	jobsDir := filepath.Join(dir, "jobs")

	// A record left behind by a previous daemon must be settled at
	// startup rather than lingering as a job that is not really running.
	stale := "20260829-010203-dead"
	if err := os.MkdirAll(filepath.Join(jobsDir, stale), 0o700); err != nil {
		t.Fatal(err)
	}
	record := `{"id":"` + stale + `","state":"running","created_at":"2026-08-29T01:02:03Z",` +
		`"spec":{"project_dir":"/tmp","workdir":"/tmp","name":"noop"}}`
	if err := os.WriteFile(filepath.Join(jobsDir, stale, daemon.RecordFile), []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- run([]string{"-socket", socketPath, "-jobs-dir", jobsDir}, devNull(t), devNull(t))
	}()

	client := daemon.NewClient(socketPath)
	deadline := time.Now().Add(10 * time.Second)
	for client.Ping() != nil {
		if time.Now().After(deadline) {
			t.Fatal("exqd never answered a ping")
		}
		time.Sleep(20 * time.Millisecond)
	}

	jobs, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].State != daemon.JobFailed {
		t.Fatalf("jobs after recovery = %+v, want the orphan marked failed", jobs)
	}

	// The signal path is what systemd uses to stop the unit.
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("exqd did not shut down on SIGTERM")
	}
}
