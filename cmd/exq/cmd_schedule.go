package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ystsbry/exq/internal/schedule"
	"github.com/ystsbry/exq/internal/systemd"
)

func newScheduleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Run commands on a schedule via systemd user timers",
		Long: `Register exq commands as systemd user timers.

A schedule is a timer plus a oneshot service that runs
"exq run <name> --bg", so a scheduled run goes through exqd exactly like
a manual background run: same job list, same logs, same stop.

The unit files under ~/.config/systemd/user are the only record of a
schedule — exq keeps no registry of its own that could disagree with
what systemd is going to fire.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newScheduleAddCmd(), newScheduleListCmd(), newScheduleRemoveCmd())
	return cmd
}

func newScheduleAddCmd() *cobra.Command {
	var onCalendar string
	cmd := &cobra.Command{
		Use:   "add <name> --on-calendar \"<expr>\" [-- <values...>]",
		Short: "Register a command to run on a calendar",
		Long: `Register a command to run on a systemd OnCalendar schedule, from the
current directory: the command runs there, with .exq/ resolved there.

The expression is systemd's own OnCalendar syntax and is validated
before anything is written:

    exq schedule add test --on-calendar "Mon..Fri 09:00"
    exq schedule add backup --on-calendar "daily"
    exq schedule add deploy --on-calendar "*-*-01 03:00" -- prod

Values after "--" are passed to the command exactly as "exq run" would
pass them.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, values, err := splitRunArgs(args, cmd.ArgsLenAtDash())
			if err != nil {
				return err
			}
			if onCalendar == "" {
				return fmt.Errorf("--on-calendar is required, e.g. --on-calendar \"Mon..Fri 09:00\"")
			}
			st, err := openStoreFromWD()
			if err != nil {
				return err
			}
			// Resolve now so a typo fails here, rather than every time the
			// timer fires from now on.
			c, err := st.Get(name)
			if err != nil {
				return err
			}
			sc, err := systemd.New()
			if err != nil {
				return err
			}
			s, err := schedule.Add(sc, schedule.Spec{
				Name:       c.Name,
				ProjectDir: st.Root,
				Workdir:    st.Root,
				OnCalendar: onCalendar,
				Values:     values,
			})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Scheduled %s as %s (%s)\n", s.Name, s.ID, s.OnCalendar)
			fmt.Fprintf(out, "  %s\n", sc.UnitPath(s.TimerUnit()))
			if next := s.NextElapse; !next.IsZero() {
				fmt.Fprintf(out, "  next run: %s\n", next.Local().Format("2006-01-02 15:04:05"))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&onCalendar, "on-calendar", "", "systemd OnCalendar expression (e.g. \"Mon..Fri 09:00\")")
	return cmd
}

func newScheduleListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List registered schedules",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := systemd.New()
			if err != nil {
				return err
			}
			schedules, err := schedule.List(sc)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(schedules) == 0 {
				fmt.Fprintln(out, "No schedules yet — add one with `exq schedule add <name> --on-calendar \"<expr>\"`.")
				return nil
			}
			writeScheduleTable(out, schedules)
			warnMissingWorkdirs(out, schedules)
			return nil
		},
	}
}

func newScheduleRemoveCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "remove <id>",
		Aliases: []string{"rm"},
		Short:   "Stop a schedule and delete its units",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := systemd.New()
			if err != nil {
				return err
			}
			s, err := schedule.Get(sc, args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if !yes && !confirmYes(cmd.InOrStdin(), out, s.ID) {
				fmt.Fprintln(out, "Cancelled.")
				return nil
			}
			if err := schedule.Remove(sc, s.ID); err != nil {
				return err
			}
			fmt.Fprintf(out, "Removed schedule %s (%s)\n", s.ID, s.Name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return cmd
}

// writeScheduleTable renders the schedules as an aligned table.
func writeScheduleTable(out io.Writer, schedules []schedule.Schedule) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tCOMMAND\tPROJECT\tON CALENDAR\tNEXT\tLAST")
	for _, s := range schedules {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			s.ID, s.Name, projectLabel(s), s.OnCalendar, nextLabel(s.NextElapse), lastLabel(s))
	}
	_ = tw.Flush()
}

// projectLabel marks a schedule whose directory is gone: it will keep
// firing and keep failing until it is removed.
func projectLabel(s schedule.Schedule) string {
	if s.WorkdirMissing {
		return s.ProjectDir + " (!)"
	}
	return s.ProjectDir
}

func nextLabel(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

// lastLabel reports how the most recent submit went. A failing oneshot
// never reaches the job history — exqd was not there to record it — so
// this line is the only place it shows up.
func lastLabel(s schedule.Schedule) string {
	if s.LastResult == "" {
		return "-"
	}
	return s.LastResult
}

// warnMissingWorkdirs spells out what the (!) marker means and what to
// do about it.
func warnMissingWorkdirs(out io.Writer, schedules []schedule.Schedule) {
	var stale []string
	for _, s := range schedules {
		if s.WorkdirMissing {
			stale = append(stale, s.ID)
		}
	}
	if len(stale) == 0 {
		return
	}
	fmt.Fprintf(out, "\n(!) missing working directory: %s\n", strings.Join(stale, ", "))
	fmt.Fprintf(out, "    these keep firing and failing — remove them with `exq schedule remove <id>`\n")
}
