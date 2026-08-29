package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ystsbry/exq/internal/daemon"
)

// connTimeout bounds one client exchange. A client that connects and
// then says nothing must not hold a goroutine forever.
const connTimeout = 10 * time.Second

// Server answers the internal/daemon protocol on a unix socket, one
// request per connection.
type Server struct {
	jobs *Jobs
	ln   net.Listener
	log  *slog.Logger

	wg sync.WaitGroup
}

// Listen creates the socket directory (0700, so only the owning user can
// reach the socket at all) and starts listening. A socket left behind by
// a crashed daemon is removed first: it is a dead file, and refusing to
// start over it would need manual cleanup on every unclean shutdown.
func Listen(socketPath string) (net.Listener, error) {
	dir := filepath.Dir(socketPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	// Tighten a directory that predates us (or a laxer umask): the socket
	// is the whole access control for the job engine.
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("chmod %s: %w", dir, err)
	}
	if err := removeStaleSocket(socketPath); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("chmod %s: %w", socketPath, err)
	}
	return ln, nil
}

// removeStaleSocket deletes a leftover socket file, but never anything
// else: a regular file at that path is a mistake worth reporting rather
// than silently deleting.
func removeStaleSocket(socketPath string) error {
	info, err := os.Lstat(socketPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%s exists and is not a socket", socketPath)
	}
	return os.Remove(socketPath)
}

// New returns a server answering on ln and executing through jobs. A nil
// logger discards the daemon's own operational log.
func New(jobs *Jobs, ln net.Listener, log *slog.Logger) *Server {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Server{jobs: jobs, ln: ln, log: log}
}

// Serve accepts connections until ctx is cancelled or the listener is
// closed, then waits for the in-flight requests. Running jobs are left
// alone: stopping them is the caller's decision (see Jobs.StopAll).
func (s *Server) Serve(ctx context.Context) error {
	// Unblock the accept loop on cancellation — a unix listener has no
	// deadline that Accept respects.
	stop := context.AfterFunc(ctx, func() { _ = s.ln.Close() })
	defer stop()

	for {
		conn, err := s.ln.Accept()
		if err != nil {
			s.wg.Wait()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handle(conn)
		}()
	}
}

// handle serves one request and closes the connection.
func (s *Server) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(connTimeout))

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		s.log.Warn("read request", "error", err)
		return
	}
	var req daemon.Request
	if err := json.Unmarshal(line, &req); err != nil {
		s.reply(conn, daemon.Response{Error: "malformed request: " + err.Error()})
		return
	}
	s.reply(conn, s.dispatch(req))
}

// dispatch executes one request. The version check happens here rather
// than in handle so a mismatched client still gets a named reason back,
// on top of the version stamped on every response.
func (s *Server) dispatch(req daemon.Request) daemon.Response {
	if req.Version != daemon.ProtocolVersion {
		return daemon.Response{Error: fmt.Sprintf(
			"protocol version mismatch: exqd speaks v%d, client sent v%d — run `exq daemon restart`",
			daemon.ProtocolVersion, req.Version)}
	}
	switch req.Op {
	case daemon.OpPing:
		return daemon.Response{OK: true}
	case daemon.OpJobSubmit:
		if req.Job == nil {
			return daemon.Response{Error: "job.submit without a job spec"}
		}
		info, err := s.jobs.Submit(*req.Job)
		if err != nil {
			return daemon.Response{Error: err.Error()}
		}
		if info.State == daemon.JobSkipped {
			// journald is where a schedule's operator looks when a run
			// seems to have gone missing.
			s.log.Info("job skipped", "job", info.ID, "name", info.Spec.Name,
				"schedule", info.Spec.ScheduleID, "reason", info.Reason)
		} else {
			s.log.Info("job submitted", "job", info.ID, "name", info.Spec.Name, "workdir", info.Spec.Workdir)
		}
		return daemon.Response{OK: true, Job: info}
	case daemon.OpJobList:
		jobs, err := s.jobs.List()
		if err != nil {
			return daemon.Response{Error: err.Error()}
		}
		return daemon.Response{OK: true, Jobs: jobs}
	case daemon.OpJobStatus:
		return jobResponse(s.jobs.Status(req.JobID))
	case daemon.OpJobStop:
		info, err := s.jobs.Stop(req.JobID)
		if err == nil {
			s.log.Info("job stop requested", "job", info.ID, "state", info.State)
		}
		return jobResponse(info, err)
	default:
		return daemon.Response{Error: fmt.Sprintf("unknown op %q", req.Op)}
	}
}

// jobResponse turns a single-job result into a response envelope.
func jobResponse(info *daemon.JobInfo, err error) daemon.Response {
	if err != nil {
		return daemon.Response{Error: err.Error()}
	}
	return daemon.Response{OK: true, Job: info}
}

// reply writes one response line. The protocol version is stamped here
// so every path — including the error paths — carries it.
func (s *Server) reply(conn net.Conn, resp daemon.Response) {
	resp.Version = daemon.ProtocolVersion
	payload, err := json.Marshal(resp)
	if err != nil {
		s.log.Error("marshal response", "error", err)
		return
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		s.log.Warn("write response", "error", err)
	}
}
