package herdr

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// request mirrors the NDJSON request shape the fake server receives.
type request struct {
	ID     string         `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

// startFakeServer listens on a unix socket, answers each connection with
// one result line (herdr serves one request per connection), and streams
// the decoded requests to the returned channel.
func startFakeServer(t *testing.T) (socketPath string, requests <-chan request) {
	t.Helper()
	// Unix socket paths are capped around 108 bytes; t.TempDir can exceed
	// that, so create a short-lived dir directly under the system tmp.
	dir, err := os.MkdirTemp("", "exq")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath = filepath.Join(dir, "herdr.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	ch := make(chan request, 16)
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
				var req request
				if json.Unmarshal(line, &req) == nil {
					ch <- req
				}
				_, _ = c.Write([]byte(`{"id":"` + req.ID + `","result":{}}` + "\n"))
			}(conn)
		}
	}()
	return socketPath, ch
}

func setHerdrEnv(t *testing.T, socketPath string) {
	t.Helper()
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", socketPath)
	t.Setenv("HERDR_PANE_ID", "w1:p1")
}

func recv(t *testing.T, ch <-chan request) request {
	t.Helper()
	select {
	case req := <-ch:
		return req
	case <-time.After(2 * time.Second):
		t.Fatal("no request received")
		return request{}
	}
}

func TestReportSendsReportAgent(t *testing.T) {
	sock, ch := startFakeServer(t)
	setHerdrEnv(t, sock)

	New().Report(StateWorking, "build prod", "step 2/5 lint")

	req := recv(t, ch)
	if req.Method != "pane.report_agent" {
		t.Errorf("method = %q, want pane.report_agent", req.Method)
	}
	want := map[string]any{
		"pane_id":       "w1:p1",
		"source":        "exq",
		"agent":         "exq",
		"state":         "working",
		"message":       "build prod",
		"custom_status": "step 2/5 lint",
	}
	for k, w := range want {
		if got := req.Params[k]; got != w {
			t.Errorf("params[%q] = %v, want %v", k, got, w)
		}
	}
	if seq, ok := req.Params["seq"].(float64); !ok || seq <= 0 {
		t.Errorf("params[seq] = %v, want positive integer", req.Params["seq"])
	}
}

func TestReportOmitsEmptyOptionalFields(t *testing.T) {
	sock, ch := startFakeServer(t)
	setHerdrEnv(t, sock)

	New().Report(StateIdle, "", "")

	req := recv(t, ch)
	if got := req.Params["state"]; got != "idle" {
		t.Errorf("params[state] = %v, want idle", got)
	}
	for _, k := range []string{"message", "custom_status"} {
		if _, present := req.Params[k]; present {
			t.Errorf("params[%q] should be omitted when empty", k)
		}
	}
}

func TestReleaseSendsReleaseAgent(t *testing.T) {
	sock, ch := startFakeServer(t)
	setHerdrEnv(t, sock)

	New().Release()

	req := recv(t, ch)
	if req.Method != "pane.release_agent" {
		t.Errorf("method = %q, want pane.release_agent", req.Method)
	}
	for k, w := range map[string]any{"pane_id": "w1:p1", "source": "exq", "agent": "exq"} {
		if got := req.Params[k]; got != w {
			t.Errorf("params[%q] = %v, want %v", k, got, w)
		}
	}
}

func TestDisabledOutsideHerdrEnv(t *testing.T) {
	sock, ch := startFakeServer(t)
	setHerdrEnv(t, sock)
	t.Setenv("HERDR_ENV", "") // socket and pane are set, but not the guard

	rep := New()
	rep.Report(StateWorking, "x", "")
	rep.Release()

	select {
	case req := <-ch:
		t.Fatalf("unexpected request outside herdr env: %+v", req)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDisabledWithoutPaneID(t *testing.T) {
	sock, ch := startFakeServer(t)
	setHerdrEnv(t, sock)
	t.Setenv("HERDR_PANE_ID", "")

	New().Report(StateWorking, "x", "")

	select {
	case req := <-ch:
		t.Fatalf("unexpected request without pane id: %+v", req)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestConnectFailureIsSilent(t *testing.T) {
	setHerdrEnv(t, filepath.Join(t.TempDir(), "missing.sock"))

	rep := New()
	// Must neither panic nor return an error path — just no-op.
	rep.Report(StateWorking, "x", "y")
	rep.Report(StateIdle, "", "")
	rep.Release()
}

func TestNilReporterIsSafe(t *testing.T) {
	var rep *Reporter
	rep.Report(StateWorking, "x", "")
	rep.Release()
}
