// Package schedule registers exq commands as systemd user timers.
//
// exq does not schedule anything itself. A schedule is a timer unit plus
// a oneshot service that runs `exq run <name> --bg`, so a scheduled run
// reaches exqd through exactly the same submit path as a manual one, and
// its state and log live with every other job. The unit files are the
// only record of a schedule: there is no second registry to drift out of
// step with what systemd is actually going to fire.
package schedule

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ystsbry/exq/internal/command"
	"github.com/ystsbry/exq/internal/systemd"
)

const (
	// unitPrefix names every unit exq generates, which is also how they
	// are found again.
	unitPrefix = "exq-sched-"
	// metaSection carries exq's own metadata inside the service unit.
	// systemd ignores sections starting with X-, so nothing here is
	// interpreted — including a literal %.
	metaSection = "X-Exq"
	// daemonUnit is what a scheduled submit needs running.
	daemonUnit = "exqd.service"
)

// Schedule is one registered schedule, reconstructed from its units.
type Schedule struct {
	ID         string
	Name       string // the exq command to run
	ProjectDir string // where .exq/ is resolved from
	Workdir    string // the working directory the command runs in
	Values     []string
	OnCalendar string
	// NextElapse is when the timer fires next; zero when systemd has no
	// answer (the timer is not loaded, or is inactive).
	NextElapse time.Time
	// LastResult is how the most recent submit attempt ended, as
	// systemd's own Result word ("success", "exit-code", …). Empty when
	// the service has not run yet.
	LastResult string
	// WorkdirMissing marks a schedule whose directory is gone: it will
	// keep firing and keep failing until it is removed.
	WorkdirMissing bool
}

// TimerUnit and ServiceUnit are the unit names of a schedule.
func (s Schedule) TimerUnit() string   { return unitPrefix + s.ID + ".timer" }
func (s Schedule) ServiceUnit() string { return unitPrefix + s.ID + ".service" }

// Spec is what registering a schedule needs.
type Spec struct {
	Name       string
	ProjectDir string
	Workdir    string
	OnCalendar string
	Values     []string
}

// Add validates the spec, writes the timer/service pair and enables it.
// The OnCalendar expression is checked first, so an invalid one leaves
// nothing behind.
func Add(sc *systemd.Client, spec Spec) (*Schedule, error) {
	if err := command.ValidateName(spec.Name); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(spec.ProjectDir) || !filepath.IsAbs(spec.Workdir) {
		return nil, errors.New("schedule needs absolute project and working directories")
	}
	if err := sc.ValidateCalendar(spec.OnCalendar); err != nil {
		return nil, err
	}
	if err := sc.Available(); err != nil {
		return nil, err
	}
	id, err := newID(sc, spec)
	if err != nil {
		return nil, err
	}
	s := &Schedule{
		ID:         id,
		Name:       spec.Name,
		ProjectDir: spec.ProjectDir,
		Workdir:    spec.Workdir,
		Values:     spec.Values,
		OnCalendar: spec.OnCalendar,
	}
	timer, service := UnitFiles(*s)
	if _, err := sc.WriteUnit(s.ServiceUnit(), service); err != nil {
		return nil, err
	}
	if _, err := sc.WriteUnit(s.TimerUnit(), timer); err != nil {
		return nil, err
	}
	if err := sc.DaemonReload(); err != nil {
		return nil, err
	}
	if err := sc.EnableNow(s.TimerUnit()); err != nil {
		// Leaving half a schedule behind would show up in `schedule list`
		// as something that never fires.
		_ = sc.RemoveUnit(s.TimerUnit())
		_ = sc.RemoveUnit(s.ServiceUnit())
		_ = sc.DaemonReload()
		return nil, err
	}
	return s, nil
}

// List returns every registered schedule, enriched with what systemd
// knows about it: the next firing time and how the last submit went.
func List(sc *systemd.Client) ([]Schedule, error) {
	names, err := systemd.ListUnitFiles(sc.UnitDir, unitPrefix+"*.timer")
	if err != nil {
		return nil, err
	}
	schedules := make([]Schedule, 0, len(names))
	for _, name := range names {
		id := strings.TrimSuffix(strings.TrimPrefix(name, unitPrefix), ".timer")
		s, err := load(sc, id)
		if err != nil {
			// A unit we cannot parse is not a reason to hide the rest.
			continue
		}
		schedules = append(schedules, *s)
	}
	return schedules, nil
}

// Get returns one schedule by id.
func Get(sc *systemd.Client, id string) (*Schedule, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	return load(sc, id)
}

// Remove stops and deletes a schedule's units.
func Remove(sc *systemd.Client, id string) error {
	s, err := Get(sc, id)
	if err != nil {
		return err
	}
	if err := sc.Available(); err != nil {
		return err
	}
	// Disabling can fail on a unit systemd never loaded; the files still
	// have to go, or `schedule list` keeps showing a dead schedule.
	disableErr := sc.DisableNow(s.TimerUnit())
	if err := sc.RemoveUnit(s.TimerUnit()); err != nil {
		return err
	}
	if err := sc.RemoveUnit(s.ServiceUnit()); err != nil {
		return err
	}
	if err := sc.DaemonReload(); err != nil {
		return err
	}
	if disableErr != nil && exists(sc.UnitPath(s.TimerUnit())) {
		return disableErr
	}
	return nil
}

// load reads a schedule's units and asks systemd for its live state.
func load(sc *systemd.Client, id string) (*Schedule, error) {
	data, err := os.ReadFile(sc.UnitPath(unitPrefix + id + ".service"))
	if err != nil {
		return nil, err
	}
	meta := parseSection(string(data), metaSection)
	s := &Schedule{
		ID:         id,
		Name:       meta["Command"],
		ProjectDir: meta["Project"],
		Workdir:    meta["Workdir"],
		OnCalendar: meta["OnCalendar"],
	}
	if raw := meta["Values"]; raw != "" {
		_ = json.Unmarshal([]byte(raw), &s.Values)
	}
	if s.Workdir != "" {
		info, err := os.Stat(s.Workdir)
		s.WorkdirMissing = err != nil || !info.IsDir()
	}
	s.NextElapse = nextElapse(sc, s.TimerUnit())
	if props, err := sc.Show(s.ServiceUnit(), "Result", "ExecMainStartTimestamp"); err == nil {
		if props["ExecMainStartTimestamp"] != "" {
			s.LastResult = props["Result"]
		}
	}
	return s, nil
}

// nextElapse reads the timer's next realtime firing point. systemd
// reports it as microseconds since the epoch, and 0 or "n/a" when it has
// none to give.
func nextElapse(sc *systemd.Client, timer string) time.Time {
	props, err := sc.Show(timer, "NextElapseUSecRealtime")
	if err != nil {
		return time.Time{}
	}
	usec, err := strconv.ParseInt(props["NextElapseUSecRealtime"], 10, 64)
	if err != nil || usec <= 0 {
		return time.Time{}
	}
	return time.UnixMicro(usec)
}

// UnitFiles renders the timer and service units of a schedule.
func UnitFiles(s Schedule) (timer, service string) {
	esc := systemd.EscapeValue
	timer = "[Unit]\n" +
		"Description=exq schedule: " + esc(s.Name) + " @ " + esc(s.ProjectDir) + "\n" +
		"\n[Timer]\n" +
		"OnCalendar=" + esc(s.OnCalendar) + "\n" +
		// Persistent catches up a firing the machine slept through, once.
		"Persistent=true\n" +
		"\n[Install]\n" +
		"WantedBy=timers.target\n"

	argv := []string{
		systemd.QuoteExecArg(systemd.BinaryPath("exq")),
		"run", systemd.QuoteExecArg(s.Name),
		"--bg", "--schedule-id", systemd.QuoteExecArg(s.ID),
	}
	if len(s.Values) > 0 {
		argv = append(argv, "--")
		for _, v := range s.Values {
			argv = append(argv, systemd.QuoteExecArg(v))
		}
	}
	values, _ := json.Marshal(s.Values)
	service = "[Unit]\n" +
		"Description=exq schedule submit: " + esc(s.Name) + "\n" +
		// The submit needs exqd; ask for it rather than assume it.
		"Wants=" + daemonUnit + "\n" +
		"After=" + daemonUnit + "\n" +
		"\n[Service]\n" +
		"Type=oneshot\n" +
		"WorkingDirectory=" + esc(s.Workdir) + "\n" +
		"ExecStart=" + strings.Join(argv, " ") + "\n" +
		// systemd ignores X- sections, so exq can keep its own record of
		// the schedule right next to the unit it generated.
		"\n[" + metaSection + "]\n" +
		"Command=" + s.Name + "\n" +
		"Project=" + s.ProjectDir + "\n" +
		"Workdir=" + s.Workdir + "\n" +
		"OnCalendar=" + s.OnCalendar + "\n" +
		"Values=" + string(values) + "\n"
	return timer, service
}

// parseSection returns the key/value pairs of one unit file section.
func parseSection(unit, section string) map[string]string {
	values := map[string]string{}
	inSection := false
	for line := range strings.SplitSeq(unit, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inSection = line == "["+section+"]"
			continue
		}
		if !inSection {
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values
}

// newID mints a schedule id from the project and command names, adding a
// numeric suffix when that pair is already scheduled — the same command
// can legitimately run on more than one calendar.
func newID(sc *systemd.Client, spec Spec) (string, error) {
	base := slug(filepath.Base(spec.ProjectDir)) + "-" + slug(spec.Name)
	if base == "-" {
		return "", fmt.Errorf("cannot derive a schedule id from %q in %q", spec.Name, spec.ProjectDir)
	}
	for i := 1; i < 1000; i++ {
		id := base
		if i > 1 {
			id = base + "-" + strconv.Itoa(i)
		}
		if !exists(sc.UnitPath(unitPrefix + id + ".timer")) {
			return id, nil
		}
	}
	return "", fmt.Errorf("too many schedules named %s", base)
}

// slug reduces a string to the lowercase, dash-separated form a unit
// name can carry.
func slug(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash && b.Len() > 0:
			b.WriteRune('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// validateID rejects ids that would address a unit outside exq's own.
func validateID(id string) error {
	if id == "" || slug(id) != id {
		return fmt.Errorf("invalid schedule id %q", id)
	}
	return nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
