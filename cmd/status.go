package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

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
	i, err := d.installation(cmd)
	if err != nil {
		return status.Report{}, err
	}
	r := status.Report{Installation: i.Name, Version: d.Build.Version}
	d.statusOfStack(cmd, &r)
	d.statusOfBackups(cmd.Context(), i, &r)
	d.statusOfTimers(cmd, i, &r)
	r.Apps, r.AppsErr = d.appAddresses(i)
	return r, nil
}

func (d Dependencies) statusOfStack(cmd *cobra.Command, r *status.Report) {
	runner, err := d.Compose(report.From(cmd.Context()).Tool("compose"), &compose.Outcomes{})
	if err != nil {
		r.StackErr = err
		return
	}
	containers, err := runner.Containers(cmd.Context(), compose.Stack.Name)
	if err != nil {
		r.StackErr = err
		return
	}
	r.Services = len(containers)
	for _, container := range containers {
		if container.State == containerRunning {
			r.Running++
		}
	}
}

func (d Dependencies) statusOfBackups(ctx context.Context, i *installation.Installation, r *status.Report) {
	b, configured, err := d.backupRole(ctx, i)
	switch {
	case err != nil:
		r.MainErr = err
	case !configured:
		r.NoBackups = true
	default:
		r.Main, r.MainErr = b.CurrentMain(ctx)
	}
}

func (d Dependencies) statusOfTimers(cmd *cobra.Command, i *installation.Installation, r *status.Report) {
	if d.Systemd == nil || !d.Systemd() {
		r.NoSystemd = true
		return
	}
	states, err := timers.Status(cmd.Context(), d.Run(cmd.ErrOrStderr(), cmd.ErrOrStderr()), i.Name)
	if err != nil {
		r.TimersErr = err
		return
	}
	for _, state := range states {
		r.Timers = append(r.Timers, status.Timer{Label: timerLabels[state.Job.Name], Status: state})
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
