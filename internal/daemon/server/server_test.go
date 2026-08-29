package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ystsbry/exq/internal/daemon"
)

// startServer runs a server on a fresh socket and returns a client for
// it. The socket lives directly under the system temp dir because unix
// socket paths are capped around 108 bytes.
func startServer(t *testing.T, j *Jobs) *daemon.Client {
	t.Helper()
	dir, err := os.MkdirTemp("", "exqd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "exqd.sock")

	ln, err := Listen(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- New(j, ln, nil).Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Serve: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Serve did not return after cancellation")
		}
	})
	return daemon.NewClient(socketPath)
}

func TestServerPing(t *testing.T) {
	c := startServer(t, newJobs(t))
	if err := c.Ping(); err != nil {
		t.Fatal(err)
	}
}

func TestServerSubmitListStatus(t *testing.T) {
	root := newProject(t, map[string]string{"noop": "#!/bin/sh\necho done\n"})
	j := newJobs(t)
	c := startServer(t, j)

	info, err := c.Submit(daemon.JobSpec{ProjectDir: root, Workdir: root, Name: "noop"})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, j, info.ID)

	got, err := c.Status(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != daemon.JobSucceeded {
		t.Fatalf("state = %q, want succeeded", got.State)
	}
	jobs, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != info.ID {
		t.Fatalf("list = %+v, want the single submitted job", jobs)
	}
}

func TestServerStop(t *testing.T) {
	root := newProject(t, map[string]string{"sleeper": "#!/bin/sh\nsleep 60\n"})
	j := newJobs(t)
	c := startServer(t, j)

	info, err := c.Submit(daemon.JobSpec{ProjectDir: root, Workdir: root, Name: "sleeper"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Stop(info.ID); err != nil {
		t.Fatal(err)
	}
	if done := waitState(t, j, info.ID); done.State != daemon.JobStopped {
		t.Fatalf("state = %q, want stopped", done.State)
	}
}

func TestServerReportsErrorsAsResponses(t *testing.T) {
	c := startServer(t, newJobs(t))
	if _, err := c.Status("nope"); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("status of an unknown job: err = %v", err)
	}
	if _, err := c.Submit(daemon.JobSpec{Name: "x"}); err == nil {
		t.Fatal("submit of an incomplete spec: want an error")
	}
}

// rawExchange sends one arbitrary line and returns the decoded reply, for
// the cases a well-behaved client can never produce.
func rawExchange(t *testing.T, socketPath, line string) daemon.Response {
	t.Helper()
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte(line + "\n")); err != nil {
		t.Fatal(err)
	}
	reply, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var resp daemon.Response
	if err := json.Unmarshal(reply, &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestServerRejectsMalformedAndMismatchedRequests(t *testing.T) {
	dir, err := os.MkdirTemp("", "exqd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "exqd.sock")
	ln, err := Listen(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = New(newJobs(t), ln, nil).Serve(ctx) }()

	if resp := rawExchange(t, socketPath, "{not json"); resp.OK || !strings.Contains(resp.Error, "malformed") {
		t.Fatalf("malformed request answered with %+v", resp)
	}
	old := rawExchange(t, socketPath, `{"version":0,"op":"ping"}`)
	if old.OK || !strings.Contains(old.Error, "restart") {
		t.Fatalf("version mismatch answered with %+v", old)
	}
	if old.Version != daemon.ProtocolVersion {
		t.Fatalf("response version = %d, want the daemon's own %d", old.Version, daemon.ProtocolVersion)
	}
	unknown := rawExchange(t, socketPath, `{"version":1,"op":"job.teleport"}`)
	if unknown.OK || !strings.Contains(unknown.Error, "job.teleport") {
		t.Fatalf("unknown op answered with %+v", unknown)
	}
}

func TestListenTightensPermissionsAndClearsStaleSockets(t *testing.T) {
	dir, err := os.MkdirTemp("", "exqd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "sub", "exqd.sock")

	ln, err := Listen(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Dir(socketPath))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("socket dir mode = %o, want 700", perm)
	}
	// Leave the socket file behind, the way a killed daemon does: Go
	// normally unlinks it on close, which is exactly what a crash skips.
	unix, ok := ln.(*net.UnixListener)
	if !ok {
		t.Fatalf("listener is %T, want *net.UnixListener", ln)
	}
	unix.SetUnlinkOnClose(false)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(socketPath); err != nil {
		t.Fatalf("stale socket was not left behind: %v", err)
	}
	again, err := Listen(socketPath)
	if err != nil {
		t.Fatalf("listening over a stale socket: %v", err)
	}
	_ = again.Close()

	// A regular file at the socket path is a mistake worth reporting, not
	// something to delete on the user's behalf.
	if err := os.WriteFile(socketPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(socketPath); err == nil {
		t.Fatal("listening over a regular file: want an error")
	}
}
