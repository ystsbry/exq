// Package systemd is the thin layer exq uses to talk to the user's own
// systemd instance: writing unit files under ~/.config/systemd/user and
// driving them through `systemctl --user`.
//
// exq only ever uses *user* units. Jobs and schedules run with the
// user's repositories, environment and permissions — a root-owned system
// unit would be both unnecessary and dangerous.
package systemd

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Client runs systemctl against the calling user's systemd instance and
// owns the directory its unit files live in.
type Client struct {
	// Systemctl and Analyze are resolved through PATH when the commands
	// run, which is also how a test stands in for them.
	Systemctl string
	Analyze   string
	// UnitDir is where exq's unit files are written.
	UnitDir string
}

// New returns a client writing units to $XDG_CONFIG_HOME/systemd/user
// (or ~/.config/systemd/user).
func New() (*Client, error) {
	dir, err := unitDir()
	if err != nil {
		return nil, err
	}
	return &Client{Systemctl: "systemctl", Analyze: "systemd-analyze", UnitDir: dir}, nil
}

// unitDir resolves the user unit directory the way systemd itself does.
func unitDir() (string, error) {
	if cfg := os.Getenv("XDG_CONFIG_HOME"); cfg != "" {
		return filepath.Join(cfg, "systemd", "user"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the systemd user unit directory: %w", err)
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}

// Available reports whether a user systemd instance is actually usable.
// It is the pre-flight check for everything else here: on WSL2 without
// systemd enabled, systemctl exists but has nothing to talk to, and the
// failure is much easier to act on when it is named up front.
func (c *Client) Available() error {
	if _, err := exec.LookPath(c.Systemctl); err != nil {
		return fmt.Errorf("%s not found — exq's background jobs and schedules need systemd", c.Systemctl)
	}
	if _, err := c.Run("show", "--property=Version"); err != nil {
		return fmt.Errorf("cannot reach a systemd user instance: %w\n"+
			"On WSL2 this usually means systemd is off: add\n\n"+
			"    [boot]\n    systemd=true\n\n"+
			"to /etc/wsl.conf and restart WSL (wsl --shutdown)", err)
	}
	return nil
}

// Run executes `systemctl --user <args...>` and returns its stdout. A
// non-zero exit is an error, with whatever systemctl said on stderr
// carried along — that message is usually the whole diagnosis.
func (c *Client) Run(args ...string) (string, error) {
	cmd := exec.Command(c.Systemctl, append([]string{"--user"}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		return stdout.String(), fmt.Errorf("systemctl --user %s: %w: %s",
			strings.Join(args, " "), err, detail)
	}
	return stdout.String(), nil
}

// Query runs a systemctl subcommand whose non-zero exit is an answer
// rather than a failure — is-active and is-enabled report the state that
// way — and returns the trimmed output ("unknown" when there is none).
func (c *Client) Query(args ...string) string {
	out, _ := c.Run(args...)
	if s := strings.TrimSpace(out); s != "" {
		return s
	}
	return "unknown"
}

// Show reads properties of a unit as a map. Absent properties simply do
// not appear in the result.
func (c *Client) Show(unit string, props ...string) (map[string]string, error) {
	args := []string{"show", unit}
	for _, p := range props {
		args = append(args, "--property="+p)
	}
	out, err := c.Run(args...)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			values[key] = value
		}
	}
	return values, nil
}

// DaemonReload makes systemd pick up unit files exq just wrote or
// removed.
func (c *Client) DaemonReload() error {
	_, err := c.Run("daemon-reload")
	return err
}

// EnableNow enables a unit and starts it in one step.
func (c *Client) EnableNow(unit string) error {
	_, err := c.Run("enable", "--now", unit)
	return err
}

// DisableNow stops a unit and removes its enablement symlinks.
func (c *Client) DisableNow(unit string) error {
	_, err := c.Run("disable", "--now", unit)
	return err
}

// Restart restarts a unit.
func (c *Client) Restart(unit string) error {
	_, err := c.Run("restart", unit)
	return err
}

// UnitPath is the on-disk location of one unit file.
func (c *Client) UnitPath(name string) string {
	return filepath.Join(c.UnitDir, name)
}

// WriteUnit writes (or overwrites) a unit file and returns its path.
// The caller still has to DaemonReload for systemd to notice.
func (c *Client) WriteUnit(name, content string) (string, error) {
	if err := os.MkdirAll(c.UnitDir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", c.UnitDir, err)
	}
	path := c.UnitPath(name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

// RemoveUnit deletes a unit file. A file that is already gone is not an
// error: removal is meant to be safe to repeat.
func (c *Client) RemoveUnit(name string) error {
	if err := os.Remove(c.UnitPath(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", c.UnitPath(name), err)
	}
	return nil
}

// BinaryPath locates one of exq's own binaries for an ExecStart line.
// The sibling of the running executable comes first, so a unit written
// by an exq in ~/.local/bin points at the exqd next to it rather than at
// whatever a future PATH happens to resolve.
func BinaryPath(name string) string {
	if self, err := os.Executable(); err == nil {
		if candidate := filepath.Join(filepath.Dir(self), name); isExecutable(candidate) {
			return candidate
		}
	}
	if found, err := exec.LookPath(name); err == nil {
		if abs, err := filepath.Abs(found); err == nil {
			return abs
		}
		return found
	}
	// Nothing installed yet: name the conventional location so the unit
	// is at least readable, and starting it fails with a clear path.
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "bin", name)
	}
	return name
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0
}

// ValidateCalendar checks an OnCalendar expression with
// `systemd-analyze calendar`, so a typo is caught at registration time
// instead of silently never firing. A missing systemd-analyze is not
// treated as a failure: refusing to register a schedule because the
// checker is absent would be worse than registering an unchecked one.
func (c *Client) ValidateCalendar(expr string) error {
	if expr == "" {
		return errors.New("empty OnCalendar expression")
	}
	if _, err := exec.LookPath(c.Analyze); err != nil {
		return nil
	}
	cmd := exec.Command(c.Analyze, "calendar", expr)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		return fmt.Errorf("invalid OnCalendar expression %q: %s", expr, detail)
	}
	return nil
}

// ListUnitFiles returns the names of exq's own unit files matching a
// glob within the unit directory. The unit files are the source of
// truth for schedules — exq keeps no separate registry that could drift
// out of step with them.
func ListUnitFiles(dir, pattern string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, filepath.Base(m))
	}
	sort.Strings(names)
	return names, nil
}

// EscapeValue makes a string safe to interpolate into a unit file: a
// literal % would otherwise be read as the start of a systemd specifier
// such as %h.
func EscapeValue(s string) string {
	return strings.ReplaceAll(s, "%", "%%")
}

// QuoteExecArg quotes one ExecStart argument. systemd splits the command
// line on whitespace unless the argument is quoted, and reads C-style
// escapes inside double quotes.
func QuoteExecArg(s string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
	return `"` + EscapeValue(escaped) + `"`
}
