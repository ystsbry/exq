package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startFakeServer listens on a unix socket, answers each connection
// with respond's one-line reply (exqd serves one request per
// connection), and streams the decoded requests to the returned
// channel.
func startFakeServer(t *testing.T, respond func(Request) Response) (socketPath string, requests <-chan Request) {
	t.Helper()
	// Unix socket paths are capped around 108 bytes; t.TempDir can exceed
	// that, so create a short-lived dir directly under the system tmp.
	dir, err := os.MkdirTemp("", "exq")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath = filepath.Join(dir, "exqd.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	ch := make(chan Request, 16)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				line, err := bufio.NewReader(c).ReadBytes('\n')
				if err != nil {
					return
				}
				var req Request
				if json.Unmarshal(line, &req) != nil {
					return
				}
				ch <- req
				payload, err := json.Marshal(respond(req))
				if err != nil {
					return
				}
				_, _ = c.Write(append(payload, '\n'))
			}(conn)
		}
	}()
	return socketPath, ch
}

// okResponse echoes the protocol version with ok plus the given body.
func okResponse(body Response) func(Request) Response {
	return func(Request) Response {
		body.Version = ProtocolVersion
		body.OK = true
		return body
	}
}

func recv(t *testing.T, ch <-chan Request) Request {
	t.Helper()
	select {
	case req := <-ch:
		return req
	case <-time.After(2 * time.Second):
		t.Fatal("no request received")
		return Request{}
	}
}

func TestSubmitSendsSpecAndReturnsJob(t *testing.T) {
	want := JobInfo{ID: "j1", State: JobQueued}
	sock, ch := startFakeServer(t, okResponse(Response{Job: &want}))

	spec := JobSpec{
		ProjectDir: "/repo",
		Workdir:    "/repo/sub",
		Name:       "build",
		Args:       []string{"prod", ""},
		ScheduleID: "exq-build",
	}
	job, err := NewClient(sock).Submit(spec)
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "j1" || job.State != JobQueued {
		t.Errorf("job = %+v, want id j1 queued", job)
	}

	req := recv(t, ch)
	if req.Op != OpJobSubmit {
		t.Errorf("op = %q, want %q", req.Op, OpJobSubmit)
	}
	if req.Version != ProtocolVersion {
		t.Errorf("version = %d, want %d", req.Version, ProtocolVersion)
	}
	if req.Job == nil {
		t.Fatal("request carried no job spec")
	}
	if got := *req.Job; got.ProjectDir != spec.ProjectDir || got.Workdir != spec.Workdir ||
		got.Name != spec.Name || got.ScheduleID != spec.ScheduleID || len(got.Args) != 2 {
		t.Errorf("spec = %+v, want %+v", got, spec)
	}
}

func TestListReturnsJobs(t *testing.T) {
	jobs := []JobInfo{
		{ID: "j1", State: JobRunning},
		{ID: "j2", State: JobSucceeded},
	}
	sock, ch := startFakeServer(t, okResponse(Response{Jobs: jobs}))

	got, err := NewClient(sock).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "j1" || got[1].State != JobSucceeded {
		t.Errorf("jobs = %+v, want %+v", got, jobs)
	}
	if req := recv(t, ch); req.Op != OpJobList {
		t.Errorf("op = %q, want %q", req.Op, OpJobList)
	}
}

func TestStatusAndStopSendJobID(t *testing.T) {
	sock, ch := startFakeServer(t, okResponse(Response{Job: &JobInfo{ID: "j1"}}))
	c := NewClient(sock)

	if _, err := c.Status("j1"); err != nil {
		t.Fatal(err)
	}
	if req := recv(t, ch); req.Op != OpJobStatus || req.JobID != "j1" {
		t.Errorf("request = %+v, want op %q job_id j1", req, OpJobStatus)
	}

	if _, err := c.Stop("j1"); err != nil {
		t.Fatal(err)
	}
	if req := recv(t, ch); req.Op != OpJobStop || req.JobID != "j1" {
		t.Errorf("request = %+v, want op %q job_id j1", req, OpJobStop)
	}
}

func TestJobOpsRequireID(t *testing.T) {
	sock, ch := startFakeServer(t, okResponse(Response{}))
	c := NewClient(sock)

	if _, err := c.Status(""); err == nil {
		t.Error("Status(\"\") should fail without contacting the daemon")
	}
	if _, err := c.Stop(""); err == nil {
		t.Error("Stop(\"\") should fail without contacting the daemon")
	}
	select {
	case req := <-ch:
		t.Fatalf("unexpected request: %+v", req)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPing(t *testing.T) {
	sock, ch := startFakeServer(t, okResponse(Response{}))
	if err := NewClient(sock).Ping(); err != nil {
		t.Fatal(err)
	}
	if req := recv(t, ch); req.Op != OpPing {
		t.Errorf("op = %q, want %q", req.Op, OpPing)
	}
}

func TestServerErrorSurfaces(t *testing.T) {
	sock, _ := startFakeServer(t, func(Request) Response {
		return Response{Version: ProtocolVersion, OK: false, Error: "no such job: j9"}
	})
	_, err := NewClient(sock).Status("j9")
	if err == nil || !strings.Contains(err.Error(), "no such job: j9") {
		t.Errorf("err = %v, want the daemon's error surfaced", err)
	}
}

func TestVersionMismatch(t *testing.T) {
	sock, _ := startFakeServer(t, func(Request) Response {
		return Response{Version: ProtocolVersion + 1, OK: true}
	})
	err := NewClient(sock).Ping()
	if !errors.Is(err, ErrVersionMismatch) {
		t.Errorf("err = %v, want ErrVersionMismatch", err)
	}
}

func TestUnreachableDaemon(t *testing.T) {
	err := NewClient(filepath.Join(t.TempDir(), "missing.sock")).Ping()
	if !errors.Is(err, ErrUnreachable) {
		t.Errorf("err = %v, want ErrUnreachable", err)
	}
}

func TestMissingJobInResponseIsAnError(t *testing.T) {
	sock, _ := startFakeServer(t, okResponse(Response{})) // ok but no job payload
	if _, err := NewClient(sock).Submit(JobSpec{Name: "x"}); err == nil {
		t.Error("Submit should fail when the response carries no job")
	}
}

func TestStalledServerBoundsBlockingToOneTimeout(t *testing.T) {
	// A server that accepts but never replies must not stall a request
	// beyond the single shared deadline (dial + write + read).
	dir, err := os.MkdirTemp("", "exq")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "exqd.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer func() { _ = conn.Close() }() // hold open, never respond
		}
	}()

	c := NewClient(socketPath)
	c.timeout = 200 * time.Millisecond
	start := time.Now()
	err = c.Ping()
	if err == nil {
		t.Error("Ping against a stalled server should fail")
	}
	if elapsed := time.Since(start); elapsed > 3*c.timeout {
		t.Errorf("Ping blocked for %v, want at most ~%v", elapsed, c.timeout)
	}
}

func TestSocketPathPrefersXDGRuntimeDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if got, want := SocketPath(), filepath.Join("/run/user/1000", "exq", "exqd.sock"); got != want {
		t.Errorf("SocketPath() = %q, want %q", got, want)
	}
}

func TestSocketPathFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("HOME", "/home/someone")
	if got, want := SocketPath(), filepath.Join("/home/someone", ".exq-daemon", "exqd.sock"); got != want {
		t.Errorf("SocketPath() = %q, want %q", got, want)
	}
}
