package status

import (
	"fmt"
	"strings"
	"time"

	"github.com/pablovarela/media-server-engine/internal/backup"
	"github.com/pablovarela/media-server-engine/internal/runs"
)

const staleBackup = 48 * time.Hour

type Timer struct {
	Label   string
	Next    time.Time
	Last    runs.Run
	State   runs.State
	Err     error
	Result  string
	Stopped time.Time
}

func (t Timer) failedInSystemd() bool {
	return t.Result != "" && t.Result != "success" && t.State != runs.Failed && t.Stopped.After(t.Last.Started)
}

type App struct{ Name, Address string }

type Report struct {
	Installation, Version string
	Main                  *backup.Main
	MainErr               error
	NoBackups             bool
	Running, Services     int
	StackErr              error
	Timers                []Timer
	NoSystemd             bool
	TimersErr             error
	Apps                  []App
	AppsErr               error
}

func (r Report) Render(now time.Time) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s on mse %s\n%s\n\n", r.Installation, r.Version, r.role())
	fmt.Fprintf(&out, "Stack: %s\n", r.stack())
	if r.Main != nil {
		fmt.Fprintf(&out, "Last backup: %s by %s\n", when(r.Main.Time, now), r.Main.Machine)
	}
	out.WriteString("\n" + r.timers(now) + "\n" + r.apps())
	return out.String()
}

func (r Report) apps() string {
	if r.AppsErr != nil {
		return "Apps: couldn't list them: " + r.AppsErr.Error() + "\n"
	}
	var out strings.Builder
	out.WriteString("Apps:\n")
	for _, app := range r.Apps {
		fmt.Fprintf(&out, "  %-12s %s\n", app.Name, app.Address)
	}
	return out.String()
}

func (r Report) role() string {
	switch {
	case r.NoBackups:
		return "No backup repository is set; mse configure sets one."
	case r.MainErr != nil:
		return "Couldn't reach the backup repository: " + r.MainErr.Error()
	case r.Main == nil:
		return "No backups yet; the first machine to back up becomes the main."
	case r.Main.ThisMachine:
		return "This machine is the main: it made the latest backup."
	}
	return r.Main.Machine + " is the main: it made the latest backup. This machine doesn't back up; mse backup --take-over makes it the main."
}

func (r Report) stack() string {
	switch {
	case r.StackErr != nil:
		return "couldn't read it: " + r.StackErr.Error()
	case r.Services == 0:
		return "no containers; mse apply starts it"
	}
	return fmt.Sprintf("%d of %d services running", r.Running, r.Services)
}

func (r Report) timers(now time.Time) string {
	switch {
	case r.NoSystemd:
		return "No timers on this machine (no systemd).\n"
	case r.TimersErr != nil:
		return "Timers: couldn't read them: " + r.TimersErr.Error() + "\n"
	}
	var out strings.Builder
	out.WriteString("Timers:\n")
	for _, timer := range r.Timers {
		fmt.Fprintf(&out, "  %-16s %-40s next %s\n", timer.Label, timer.lastRun(now), when(timer.Next, now))
	}
	return out.String()
}

func (t Timer) lastRun(now time.Time) string {
	started := when(t.Last.Started, now)
	switch {
	case t.Err != nil:
		return "couldn't read its last run: " + t.Err.Error()
	case t.failedInSystemd():
		return "systemd couldn't run it (" + t.Result + "), " + when(t.Stopped, now)
	case t.State == runs.NeverRan:
		return "hasn't run yet"
	case t.State == runs.Running:
		return "running now, since " + started
	case t.State == runs.Interrupted:
		return "started " + started + ", stopped before finishing"
	case t.State == runs.Failed:
		return "last ran " + started + ", failed"
	}
	return "last ran " + started + ", succeeded"
}

func when(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.In(now.Location()).Format("2 Jan 15:04")
}

func (r Report) Attention(now time.Time) []string {
	return append(append(r.timersAttention(), r.stackAttention()...), r.backupAttention(now)...)
}

func (r Report) timersAttention() []string {
	var needs []string
	for _, timer := range r.Timers {
		switch {
		case timer.Err != nil:
			needs = append(needs, timer.Label+"'s last run couldn't be read")
		case timer.failedInSystemd():
			needs = append(needs, "systemd couldn't run "+timer.Label+" ("+timer.Result+")")
		case timer.State == runs.Failed:
			needs = append(needs, timer.Label+"'s last run failed")
		case timer.State == runs.Interrupted:
			needs = append(needs, timer.Label+"'s last run stopped before finishing")
		}
	}
	if r.TimersErr != nil {
		needs = append(needs, "the timers couldn't be read")
	}
	return needs
}

func (r Report) stackAttention() []string {
	switch {
	case r.StackErr != nil:
		return []string{"the stack couldn't be read"}
	case r.Services == 0:
		return []string{"the stack isn't running"}
	case r.Services-r.Running == 1:
		return []string{fmt.Sprintf("1 of %d services isn't running", r.Services)}
	case r.Running < r.Services:
		return []string{fmt.Sprintf("%d of %d services aren't running", r.Services-r.Running, r.Services)}
	}
	return nil
}

func (r Report) backupAttention(now time.Time) []string {
	var needs []string
	if r.MainErr != nil {
		needs = append(needs, "the backup repository couldn't be read")
	}
	if r.Main != nil && r.Main.ThisMachine && now.Sub(r.Main.Time) > staleBackup {
		needs = append(needs, "the latest backup is more than 2 days old")
	}
	return needs
}
