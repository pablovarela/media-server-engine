package cmd

import (
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"

	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"

	"github.com/pablovarela/media-server-engine/internal/report"
	"github.com/pablovarela/media-server-engine/internal/timers"
)

func newInstallTimersCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "install-timers",
		Short: "Install the systemd timers that update, back up and clean up this installation unattended",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			i, err := deps.installation(cmd)
			if err != nil {
				return err
			}
			if deps.Systemd == nil || !deps.Systemd() {
				return errors.New("timers need systemd, and systemd is not running on this machine")
			}
			if !semver.IsValid(deps.Build.Version) {
				return errors.New("a dev build can't run timers: mse update can't replace it; install a release with install.sh")
			}
			values, err := deps.timerValues(i.Name)
			if err != nil {
				return err
			}
			step := report.From(cmd.Context()).Step("Installing the timers")
			installer := timers.Installer{Runner: deps.Run(io.Discard, io.Discard), Dir: deps.UnitDir, Read: os.ReadFile}
			outcome, err := installer.Install(cmd.Context(), i.Role() == "main", values)
			if err != nil {
				return step.Fail(err)
			}
			step.Done(outcome.String())
			return nil
		},
	}
}

func (d Dependencies) timerValues(installation string) (timers.Values, error) {
	executable, err := d.Executable()
	if err != nil {
		return timers.Values{}, err
	}
	if executable, err = filepath.EvalSymlinks(executable); err != nil {
		return timers.Values{}, err
	}
	name, group, err := d.Account()
	if err != nil {
		return timers.Values{}, err
	}
	return timers.Values{User: name, Group: group, Installation: installation, Executable: executable}, nil
}

func currentAccount() (string, string, error) {
	current, err := user.Current()
	if err != nil {
		return "", "", err
	}
	group, err := user.LookupGroupId(current.Gid)
	if err != nil {
		return "", "", err
	}
	return current.Username, group.Name, nil
}
