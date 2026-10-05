package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"

	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/process"
	"github.com/pablovarela/media-server-engine/internal/report"
	"github.com/pablovarela/media-server-engine/internal/timers"
)

const systemdPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

var carriedIntoUnits = []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "SOPS_AGE_KEY_FILE", "SOPS_AGE_KEY_CMD"}

var errNoUnattendedToken = errors.New("the nightly mse update --apply can't get a GitHub token without a login: run gh auth login on this machine " +
	"(gh keeps the token in ~/.config/gh/hosts.yml when there is no keyring); a GITHUB_TOKEN set in the shell doesn't reach the timers")

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
			values, err := deps.timersCanRun(cmd.Context(), i)
			if err != nil {
				return err
			}
			b, err := deps.backups(cmd, telling)
			if err != nil {
				return err
			}
			main, err := b.RunsBackups(cmd.Context())
			if err != nil {
				return err
			}
			return deps.installTimers(cmd, main, values)
		},
	}
}

func (d Dependencies) timersCanRun(ctx context.Context, i *installation.Installation) (timers.Values, error) {
	if d.Systemd == nil || !d.Systemd() {
		return timers.Values{}, errors.New("timers need systemd, and systemd is not running on this machine")
	}
	if !semver.IsValid(d.Build.Version) {
		return timers.Values{}, errors.New("a dev build can't run timers: mse update can't replace it; install a release with install.sh")
	}
	if d.Environment("SOPS_AGE_KEY") != "" {
		return timers.Values{}, errors.New("SOPS_AGE_KEY can't reach the timers without writing the key into a unit: keep the key in a file and set SOPS_AGE_KEY_FILE instead")
	}
	values, err := d.timerValues(i.Name)
	if err != nil {
		return timers.Values{}, err
	}
	return values, d.unattendedToken(ctx, values)
}

func (d Dependencies) unattendedToken(ctx context.Context, values timers.Values) error {
	args := append([]string{"-i", "HOME=" + d.Home, "USER=" + values.User, "PATH=" + systemdPath}, values.Environment...)
	result, err := d.Run(io.Discard, io.Discard).Output(ctx, process.Command{Name: "env", Args: append(args, "gh", "auth", "token")})
	if err != nil || result.Exit != 0 || strings.TrimSpace(string(result.Stdout)) == "" {
		return errNoUnattendedToken
	}
	return nil
}

func (d Dependencies) installTimers(cmd *cobra.Command, main bool, values timers.Values) error {
	asked, err := d.Run(cmd.OutOrStdout(), cmd.ErrOrStderr()).Run(cmd.Context(), process.Command{Name: "sudo", Args: []string{"-v"}})
	if err != nil {
		return err
	}
	if asked != 0 {
		return fmt.Errorf("sudo -v failed (exit %d)", asked)
	}
	step := report.From(cmd.Context()).Step("Installing the timers")
	installer := timers.Installer{Runner: d.Run(io.Discard, io.Discard), Dir: d.UnitDir, Read: os.ReadFile}
	outcome, err := installer.Install(cmd.Context(), main, values)
	if err != nil {
		return step.Fail(err)
	}
	step.Done(outcome.String())
	return nil
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
	var environment []string
	for _, variable := range carriedIntoUnits {
		if value := d.Environment(variable); value != "" {
			environment = append(environment, variable+"="+value)
		}
	}
	return timers.Values{User: name, Group: group, Installation: installation, Executable: executable, Environment: environment}, nil
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
