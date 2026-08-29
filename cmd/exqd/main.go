// Command exqd is the exq background job daemon. It runs as a systemd
// user service, listens on a unix socket for job submissions from exq
// (manual `exq run --bg` and schedule timers alike), and executes them
// as its own child processes so they outlive the terminal that started
// them.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ystsbry/exq/internal/daemon"
	"github.com/ystsbry/exq/internal/daemon/server"
)

var (
	version = "0.1.0-dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// run is main without the exit, so the daemon's startup path is testable.
func run(args []string, stdout, stderr *os.File) error {
	fs := flag.NewFlagSet("exqd", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		socketPath  = fs.String("socket", daemon.SocketPath(), "unix socket to listen on")
		jobsDir     = fs.String("jobs-dir", daemon.JobsDir(), "directory job records and logs are kept in")
		showVersion = fs.Bool("version", false, "print the exqd version and exit")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Fprintf(stdout, "exqd %s (commit %s, built %s, protocol v%d)\n",
			version, commit, date, daemon.ProtocolVersion)
		return nil
	}

	// exqd's own log goes to stderr, which systemd routes to journald.
	// Job output never comes here: it belongs to the job's log file.
	log := slog.New(slog.NewTextHandler(stderr, nil))

	jobs, err := server.NewJobs(*jobsDir)
	if err != nil {
		return err
	}
	// Records still saying running belong to a previous exqd: its
	// children died with its cgroup, so settle them before serving.
	if n, err := jobs.Recover(); err != nil {
		log.Warn("recover jobs", "error", err)
	} else if n > 0 {
		log.Info("marked orphaned jobs as failed", "count", n)
	}

	ln, err := server.Listen(*socketPath)
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info("exqd started", "socket", *socketPath, "jobs", *jobsDir, "protocol", daemon.ProtocolVersion)
	err = server.New(jobs, ln, log).Serve(ctx)
	// systemd's default KillMode takes the whole cgroup down with us, so
	// running jobs are already doomed; stopping them here makes them end
	// as stopped rather than as records frozen mid-run.
	jobs.StopAll()
	log.Info("exqd stopped")
	return err
}
