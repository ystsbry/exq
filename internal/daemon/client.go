package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// timeout bounds one whole request — dial, write, and read share a
// single deadline — so a wedged daemon can never stall exq for long.
// Every operation is a quick state exchange; execution itself happens
// asynchronously in exqd.
const timeout = 3 * time.Second

var (
	// ErrUnreachable wraps a failure to reach the daemon socket at all,
	// so callers can suggest `exq daemon install` / `status`.
	ErrUnreachable = errors.New("exqd is not reachable")
	// ErrVersionMismatch means exq and exqd disagree on ProtocolVersion,
	// so callers can suggest `exq daemon restart`.
	ErrVersionMismatch = errors.New("exqd protocol version mismatch")
)

// SocketPath returns the default exqd socket location:
// $XDG_RUNTIME_DIR/exq/exqd.sock, or ~/.exq-daemon/exqd.sock when
// XDG_RUNTIME_DIR is unset.
func SocketPath() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "exq", "exqd.sock")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".exq-daemon", "exqd.sock")
	}
	return filepath.Join(home, ".exq-daemon", "exqd.sock")
}

// Client performs requests against an exqd socket.
type Client struct {
	socketPath string
	timeout    time.Duration
}

// NewClient returns a client for the exqd socket. An empty socketPath
// selects the default location (SocketPath).
func NewClient(socketPath string) *Client {
	if socketPath == "" {
		socketPath = SocketPath()
	}
	return &Client{socketPath: socketPath, timeout: timeout}
}

// Ping checks that the daemon is up and speaks our protocol version.
func (c *Client) Ping() error {
	_, err := c.roundTrip(Request{Op: OpPing})
	return err
}

// Submit hands a job to the daemon and returns its record (normally in
// state queued or running); the job itself runs asynchronously.
func (c *Client) Submit(spec JobSpec) (*JobInfo, error) {
	return c.jobResponse(Request{Op: OpJobSubmit, Job: &spec})
}

// List returns the daemon's job records, running and finished.
func (c *Client) List() ([]JobInfo, error) {
	resp, err := c.roundTrip(Request{Op: OpJobList})
	if err != nil {
		return nil, err
	}
	return resp.Jobs, nil
}

// Status returns the current record of one job.
func (c *Client) Status(jobID string) (*JobInfo, error) {
	if jobID == "" {
		return nil, fmt.Errorf("exqd: %s requires a job id", OpJobStatus)
	}
	return c.jobResponse(Request{Op: OpJobStatus, JobID: jobID})
}

// Stop asks the daemon to terminate a running job and returns its
// record after the stop was initiated.
func (c *Client) Stop(jobID string) (*JobInfo, error) {
	if jobID == "" {
		return nil, fmt.Errorf("exqd: %s requires a job id", OpJobStop)
	}
	return c.jobResponse(Request{Op: OpJobStop, JobID: jobID})
}

func (c *Client) jobResponse(req Request) (*JobInfo, error) {
	resp, err := c.roundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.Job == nil {
		return nil, fmt.Errorf("exqd: %s response carried no job", req.Op)
	}
	return resp.Job, nil
}

// roundTrip performs one request on a fresh connection — exqd serves
// one request per connection — and validates the response envelope.
func (c *Client) roundTrip(req Request) (*Response, error) {
	req.Version = ProtocolVersion
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(c.timeout)
	d := net.Dialer{Deadline: deadline}
	conn, err := d.Dial("unix", c.socketPath)
	if err != nil {
		return nil, fmt.Errorf("%w at %s: %v", ErrUnreachable, c.socketPath, err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		return nil, fmt.Errorf("exqd: write: %w", err)
	}
	// A daemon that closes without a trailing newline still gets its
	// partial line decoded; only an empty read is a transport error.
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil, fmt.Errorf("exqd: read: %w", err)
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("exqd: malformed response: %w", err)
	}
	if resp.Version != ProtocolVersion {
		return nil, fmt.Errorf("%w: client speaks v%d, daemon answered v%d", ErrVersionMismatch, ProtocolVersion, resp.Version)
	}
	if !resp.OK {
		return nil, fmt.Errorf("exqd: %s", resp.Error)
	}
	return &resp, nil
}
