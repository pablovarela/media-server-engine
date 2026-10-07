package status

import (
	"fmt"
	"strings"
	"time"

	"github.com/pablovarela/media-server-engine/internal/backup"
	"github.com/pablovarela/media-server-engine/internal/timers"
)

const staleBackup = 48 * time.Hour

type Timer struct {
	Label  string
	Status timers.JobStatus
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
}

func (r Report) Render(now time.Time) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s on mse %s\n%s\n\n", r.Installation, r.Version, r.role())
	fmt.Fprintf(&out, "Stack: %s\n", r.stack())
	if r.Main != nil {
		fmt.Fprintf(&out, "Last backup: %s by %s\n", when(r.Main.Time, now), r.Main.Machine)
	}
	out.WriteString("\n" + r.timers(now) + "\nApps:\n")
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
	if r.StackErr != nil {
		return "couldn't read it: " + r.StackErr.Error()
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
		ran := "hasn't run yet"
		if !timer.Status.LastRun.IsZero() {
			outcome := "succeeded"
			if !timer.Status.Succeeded {
				outcome = "failed"
			}
			ran = "last ran " + when(timer.Status.LastRun, now) + ", " + outcome
		}
		fmt.Fprintf(&out, "  %-16s %-36s next %s\n", timer.Label, ran, when(timer.Status.Next, now))
	}
	return out.String()
}

func when(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.In(now.Location()).Format("2 Jan 15:04")
}

func (r Report) Attention(now time.Time) []string {
	var needs []string
	for _, timer := range r.Timers {
		if !timer.Status.LastRun.IsZero() && !timer.Status.Succeeded {
			needs = append(needs, timer.Label+"'s last run failed")
		}
	}
	switch {
	case r.StackErr != nil:
		needs = append(needs, "the stack couldn't be read")
	case r.Services-r.Running == 1:
		needs = append(needs, fmt.Sprintf("1 of %d services isn't running", r.Services))
	case r.Running < r.Services:
		needs = append(needs, fmt.Sprintf("%d of %d services aren't running", r.Services-r.Running, r.Services))
	}
	if r.Main != nil && r.Main.ThisMachine && now.Sub(r.Main.Time) > staleBackup {
		needs = append(needs, "the latest backup is more than 2 days old")
	}
	return needs
}
