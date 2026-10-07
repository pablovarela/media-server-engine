package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/backup"
	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/report"
	"github.com/pablovarela/media-server-engine/internal/status"
	"github.com/pablovarela/media-server-engine/internal/timers"
)

var apps = []struct {
	name string
	port int
}{
	{"Jellyfin", 8096}, {"Seerr", 5055}, {"Sonarr", 8989}, {"Radarr", 7878}, {"Prowlarr", 9696},
	{"Bazarr", 6767}, {"Deluge", 8112}, {"Maintainerr", 6246}, {"Portainer", 9000},
}

var timerLabels = map[string]string{
	timers.Update.Name: "update", timers.Backup.Name: "backup", timers.Verify.Name: "check-backup", timers.Cleanup.Name: "clean-downloads",
}

func newStatusCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show this machine's installation: version, main, stack, last backup, timers and the apps' addresses",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := deps.status(cmd)
			if err != nil {
				return err
			}
			now := deps.Now()
			out := r.Render(now)
			if needs := r.Attention(now); len(needs) > 0 {
				out += "\nNeeds attention:\n  " + strings.Join(needs, "\n  ") + "\n"
				_, _ = fmt.Fprint(report.From(cmd.Context()).Data(), out)
				return errAlreadyReported
			}
			_, err = fmt.Fprint(report.From(cmd.Context()).Data(), out)
			return err
		},
	}
}

func (d Dependencies) status(cmd *cobra.Command) (status.Report, error) {
	o, err := d.openProject(cmd, compose.Stack, noDrawing)
	if err != nil {
		return status.Report{}, err
	}
	i := o.installation
	r := status.Report{Installation: i.Name, Version: d.Build.Version, Services: len(o.project.Services)}
	running, err := o.runner.RunningServices(cmd.Context(), o.project)
	r.Running, r.StackErr = len(running), err
	d.statusOfBackups(cmd.Context(), i, &r)
	d.statusOfTimers(cmd, i, &r)
	r.Apps, err = d.appAddresses(i)
	return r, err
}

func (d Dependencies) statusOfBackups(ctx context.Context, i *installation.Installation, r *status.Report) {
	b, configured, err := d.backupRole(ctx, i)
	switch {
	case err != nil:
		r.MainErr = err
	case !configured:
		r.NoBackups = true
	default:
		if b.MachineID, err = backup.MachineID(d.MachineIDFile, i.Data); err != nil {
			r.MainErr = err
			return
		}
		r.Main, r.MainErr = b.CurrentMain(ctx)
	}
}

func (d Dependencies) statusOfTimers(cmd *cobra.Command, i *installation.Installation, r *status.Report) {
	if d.Systemd == nil || !d.Systemd() {
		r.NoSystemd = true
		return
	}
	runner := d.Run(cmd.ErrOrStderr(), cmd.ErrOrStderr())
	for _, job := range timers.Jobs {
		state, err := timers.Status(cmd.Context(), runner, i.Name, job)
		if err != nil {
			r.TimersErr = err
			return
		}
		if state.Installed {
			r.Timers = append(r.Timers, status.Timer{Label: timerLabels[job.Name], Status: state})
		}
	}
}

func (d Dependencies) appAddresses(i *installation.Installation) ([]status.App, error) {
	host, err := i.NetworkName(d.Host)
	if err != nil {
		return nil, err
	}
	pinned, err := compose.HomepagePinned(i)
	if err != nil {
		return nil, err
	}
	var list []status.App
	if pinned {
		list = append(list, status.App{Name: "Home", Address: homepageAddress(i, host)})
	}
	for _, app := range apps {
		list = append(list, status.App{Name: app.name, Address: fmt.Sprintf("http://%s:%d", host, app.port)})
	}
	return list, nil
}

func homepageAddress(i *installation.Installation, host string) string {
	address := "http://" + host
	if port := i.HomepagePort(); port != "80" {
		address += ":" + port
	}
	return address
}
