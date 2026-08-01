// Package herdr reports exq's pane state to a surrounding herdr session
// through its self-report socket API (pane.report_agent /
// pane.release_agent), so the herdr sidebar shows the pane as idle /
// working / done. Reporting is strictly best-effort: outside herdr
// (HERDR_ENV unset) the reporter is a no-op, and any socket failure is
// swallowed silently — exq's own behavior must never depend on it.
package herdr

import (
	"encoding/json"
	"net"
	"os"
	"time"
)

// State is a pane agent state understood by herdr. A working → idle
// transition is what herdr renders as "done" in the sidebar.
type State string

const (
	StateIdle    State = "idle"
	StateWorking State = "working"
)

// source identifies exq in every report, as both the reporting source
// and the agent name shown in the sidebar.
const source = "exq"

// timeout bounds each socket operation so a wedged herdr server can
// never stall exq for more than a beat.
const timeout = 500 * time.Millisecond

// Reporter sends state reports to the herdr socket. A nil or disabled
// Reporter is safe to call: every method is a no-op.
type Reporter struct {
	socketPath string
	paneID     string
}

// New returns a Reporter wired to the surrounding herdr pane, or a
// disabled one when exq is not running inside herdr (HERDR_ENV != 1 or
// the socket/pane variables are missing).
func New() *Reporter {
	if os.Getenv("HERDR_ENV") != "1" {
		return &Reporter{}
	}
	sock, pane := os.Getenv("HERDR_SOCKET_PATH"), os.Getenv("HERDR_PANE_ID")
	if sock == "" || pane == "" {
		return &Reporter{}
	}
	return &Reporter{socketPath: sock, paneID: pane}
}

func (r *Reporter) enabled() bool {
	return r != nil && r.socketPath != ""
}

// Report registers or updates exq's agent row: the state plus an
// optional message (the running command) and custom status (workflow
// step progress).
func (r *Reporter) Report(state State, message, customStatus string) {
	if !r.enabled() {
		return
	}
	params := map[string]any{
		"pane_id": r.paneID,
		"source":  source,
		"agent":   source,
		"state":   state,
		"seq":     uint64(time.Now().UnixNano()),
	}
	if message != "" {
		params["message"] = message
	}
	if customStatus != "" {
		params["custom_status"] = customStatus
	}
	r.send("pane.report_agent", params)
}

// Release removes exq's agent row, used when the TUI exits without
// running anything so no stale entry lingers in the sidebar.
func (r *Reporter) Release() {
	if !r.enabled() {
		return
	}
	r.send("pane.release_agent", map[string]any{
		"pane_id": r.paneID,
		"source":  source,
		"agent":   source,
		"seq":     uint64(time.Now().UnixNano()),
	})
}

// send performs one request on a fresh connection — herdr serves one
// request per connection — and discards the response line.
func (r *Reporter) send(method string, params map[string]any) {
	payload, err := json.Marshal(map[string]any{
		"id":     "exq_report",
		"method": method,
		"params": params,
	})
	if err != nil {
		return
	}
	conn, err := net.DialTimeout("unix", r.socketPath, timeout)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		return
	}
	// Drain the one-line reply so the request/response cycle completes
	// before the connection closes; the content is irrelevant.
	buf := make([]byte, 4096)
	_, _ = conn.Read(buf)
}
