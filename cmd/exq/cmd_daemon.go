package main

import (
	"errors"
	"fmt"
	"io"
	"os/user"
	"time"

	"github.com/spf13/cobra"

	"github.com/ystsbry/exq/internal/daemon"
	"github.com/ystsbry/exq/internal/systemd"
)

// daemonUnit is the systemd user unit exqd runs as.
const daemonUnit = "exqd.service"

// pingRetry bounds how long `daemon install` waits for the freshly
// started exqd to answer: systemctl returns as soon as the process is
// spawned, a moment before the socket exists.
const (
	pingRetry    = 5 * time.Second
	pingInterval = 100 * time.Millisecond
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Install and inspect the exqd background job daemon",
		Long: `Manage exqd, the daemon that runs exq's background jobs.

exqd runs as a systemd *user* unit: it starts with your session and runs
your jobs with your own repositories, environment and permissions. There
is no system-wide service and nothing runs as root.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newDaemonInstallCmd(), newDaemonStatusCmd(), newDaemonRestartCmd())
	return cmd
}

func newDaemonInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Write the exqd user unit and start it",
		Long: `Write ~/.config/systemd/user/exqd.service, reload systemd and enable the
unit so exqd starts now and on every login.

Safe to re-run: the unit file is rewritten and the daemon restarted with
whatever exqd is currently installed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return installDaemon(cmd.OutOrStdout())
		},
	}
}

func newDaemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the exqd unit state and check the socket",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return daemonStatus(cmd.OutOrStdout())
		},
	}
}

func newDaemonRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart exqd (use after upgrading exq)",
		Long: `Restart the exqd unit. This is what a protocol version mismatch asks
for: exq and exqd are separate binaries, and an upgraded exq keeps
talking to the exqd that is still running until it is restarted.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := systemd.New()
			if err != nil {
				return err
			}
			if err := sc.Available(); err != nil {
				return err
			}
			if err := sc.Restart(daemonUnit); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Restarted %s\n", daemonUnit)
			reportPing(out, waitForDaemon())
			return nil
		},
	}
}

// currentUser is the name loginctl expects, falling back to a
// placeholder rather than failing an otherwise successful install.
func currentUser() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return "$USER"
}

// installDaemon writes the unit, starts it, and confirms the daemon
// actually answers before declaring success.
func installDaemon(out io.Writer) error {
	sc, err := systemd.New()
	if err != nil {
		return err
	}
	if err := sc.Available(); err != nil {
		return err
	}
	exqd := systemd.BinaryPath("exqd")
	path, err := sc.WriteUnit(daemonUnit, daemonUnitFile(exqd))
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Wrote %s (ExecStart=%s)\n", path, exqd)
	if err := sc.DaemonReload(); err != nil {
		return err
	}
	if err := sc.EnableNow(daemonUnit); err != nil {
		return err
	}
	fmt.Fprintf(out, "Enabled and started %s\n", daemonUnit)
	reportPing(out, waitForDaemon())

	fmt.Fprintln(out)
	fmt.Fprintln(out, "For schedules to keep firing while you are logged out, enable lingering:")
	fmt.Fprintf(out, "    loginctl enable-linger %s\n", currentUser())
	return nil
}

// daemonStatus prints what systemd thinks of the unit and what the
// socket answers — the two can disagree, and which one is wrong is the
// whole diagnosis.
func daemonStatus(out io.Writer) error {
	sc, err := systemd.New()
	if err != nil {
		return err
	}
	if err := sc.Available(); err != nil {
		return err
	}
	fmt.Fprintf(out, "unit:    %s\n", daemonUnit)
	fmt.Fprintf(out, "file:    %s\n", sc.UnitPath(daemonUnit))
	fmt.Fprintf(out, "enabled: %s\n", sc.Query("is-enabled", daemonUnit))
	fmt.Fprintf(out, "active:  %s\n", sc.Query("is-active", daemonUnit))
	fmt.Fprintf(out, "socket:  %s\n", daemon.SocketPath())
	reportPing(out, daemonClient().Ping())
	return nil
}

// reportPing turns the result of a ping into a line about the protocol,
// with the fix attached when there is one.
func reportPing(out io.Writer, err error) {
	switch {
	case err == nil:
		fmt.Fprintf(out, "ping:    ok (protocol v%d)\n", daemon.ProtocolVersion)
	case errors.Is(err, daemon.ErrVersionMismatch):
		fmt.Fprintf(out, "ping:    %v\n", err)
		fmt.Fprintf(out, "         run `exq daemon restart` so exqd picks up the installed version\n")
	default:
		fmt.Fprintf(out, "ping:    %v\n", err)
	}
}

// waitForDaemon pings until the daemon answers or the retry budget runs
// out. systemctl returns once the process is spawned, which is a moment
// before the socket is there.
func waitForDaemon() error {
	deadline := time.Now().Add(pingRetry)
	for {
		err := daemonClient().Ping()
		if err == nil || !errors.Is(err, daemon.ErrUnreachable) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(pingInterval)
	}
}

// daemonUnitFile is the exqd unit. Job output never reaches journald —
// exqd writes it to per-job log files — so only the daemon's own
// operational log lands there.
func daemonUnitFile(exqd string) string {
	return `[Unit]
Description=exq background job daemon
Documentation=https://github.com/ystsbry/exq

[Service]
Type=simple
ExecStart=` + exqd + `
Restart=on-failure
RestartSec=1

[Install]
WantedBy=default.target
`
}
