package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"

	"github.com/pablovarela/media-server-engine/internal/apply"
	"github.com/pablovarela/media-server-engine/internal/backup"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/process"
	"github.com/pablovarela/media-server-engine/internal/report"
	"github.com/pablovarela/media-server-engine/internal/secrets"
	"github.com/pablovarela/media-server-engine/internal/timers"
)

const systemdPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

var carriedIntoUnits = []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "SOPS_AGE_KEY_FILE", "SOPS_AGE_KEY_CMD"}

const noUnattendedToken = "the nightly update can't get a GitHub token without a login: run gh auth login " +
	"(gh keeps the token in ~/.config/gh/hosts.yml when there is no keyring); a GITHUB_TOKEN in the shell doesn't reach the timers"

type appliedTimers struct {
	d   Dependencies
	cmd *cobra.Command
	i   *installation.Installation
}

func (d Dependencies) timersOrNil(cmd *cobra.Command, i *installation.Installation) apply.Timers {
	if d.Systemd == nil || !d.Systemd() {
		return nil
	}
	return &appliedTimers{d: d, cmd: cmd, i: i}
}

func (t *appliedTimers) Set(ctx context.Context) (string, []string, error) {
	if !semver.IsValid(t.d.Build.Version) {
		return "skipped: a dev build can't be updated by mse update", nil, nil
	}
	account, err := t.d.Account()
	if err != nil {
		return "", nil, err
	}
	if !t.lingering(ctx, account) {
		return "skipped: they need lingering, once: sudo loginctl enable-linger " + account, nil, nil
	}
	if t.d.Environment("XDG_RUNTIME_DIR") == "" {
		return "skipped: no user session here (XDG_RUNTIME_DIR isn't set); run mse apply from a login or let the timers run it", nil, nil
	}
	if t.d.Environment("SOPS_AGE_KEY") != "" {
		return "skipped: SOPS_AGE_KEY can't reach the timers; keep the key in a file and set SOPS_AGE_KEY_FILE", nil, nil
	}
	values, err := t.values()
	if err != nil {
		return "", nil, err
	}
	warnings := t.droppedVariables()
	if !t.unattendedToken(ctx, account, values) {
		warnings = append(warnings, noUnattendedToken)
	}
	role, note, warning := t.role(ctx)
	if warning != "" {
		warnings = append(warnings, warning)
	}
	installer := timers.Installer{Runner: t.d.Run(io.Discard, io.Discard), Dir: t.d.unitDir()}
	outcome, err := installer.Install(ctx, role, values)
	if err != nil {
		return "", warnings, err
	}
	return outcome.String() + note, warnings, nil
}

func (t *appliedTimers) lingering(ctx context.Context, account string) bool {
	result, err := t.d.Run(io.Discard, io.Discard).Output(ctx, process.Command{Name: "loginctl", Args: []string{"show-user", account, "-p", "Linger"}})
	return err == nil && result.Exit == 0 && strings.TrimSpace(string(result.Stdout)) == "Linger=yes"
}

func (t *appliedTimers) values() (timers.Values, error) {
	executable, err := t.d.Executable()
	if err != nil {
		return timers.Values{}, err
	}
	if executable, err = filepath.EvalSymlinks(executable); err != nil {
		return timers.Values{}, err
	}
	var environment []string
	for _, variable := range carriedIntoUnits {
		if value := t.d.Environment(variable); value != "" {
			environment = append(environment, variable+"="+value)
		}
	}
	return timers.Values{Installation: t.i.Name, Executable: executable, Environment: environment}, nil
}

func (t *appliedTimers) unattendedToken(ctx context.Context, account string, values timers.Values) bool {
	args := append([]string{"-i", "HOME=" + t.d.Home, "USER=" + account, "PATH=" + systemdPath}, values.Environment...)
	result, err := t.d.Run(io.Discard, io.Discard).Output(ctx, process.Command{Name: "env", Args: append(args, "gh", "auth", "token")})
	return err == nil && result.Exit == 0 && strings.TrimSpace(string(result.Stdout)) != ""
}

func (t *appliedTimers) droppedVariables() []string {
	current, err := os.ReadFile(filepath.Join(t.d.unitDir(), timers.Update.Unit(t.i.Name)+".service"))
	if err != nil {
		return nil
	}
	var warnings []string
	for _, variable := range carriedIntoUnits {
		if strings.Contains(string(current), `Environment="`+variable+"=") && t.d.Environment(variable) == "" {
			warnings = append(warnings, fmt.Sprintf("the timers had %s set and this shell doesn't set it, so they now run without it; export it and run mse apply again to keep it", variable))
		}
	}
	return warnings
}

const noBackupsYet = "; no backups yet (mse claim-backup-main makes this machine the main)"

func (t *appliedTimers) role(ctx context.Context) (role timers.Role, note, warning string) {
	b, configured, err := t.d.backupRole(t.i)
	if err == nil && !configured {
		return timers.Secondary, "", ""
	}
	if err == nil {
		var runs, backedUp bool
		if runs, backedUp, err = b.RunsBackups(ctx); err == nil {
			switch {
			case runs:
				return timers.Main, "", ""
			case !backedUp:
				return timers.Secondary, noBackupsYet, ""
			}
			return timers.Secondary, "", ""
		}
	}
	return timers.Unknown, "", fmt.Sprintf("%v; the backup timers are left as they are", err)
}

func (d Dependencies) backupRole(i *installation.Installation) (*backup.Backups, bool, error) {
	location := i.Settings["RESTIC_REPOSITORY"]
	environment := map[string]string{}
	if decrypted, err := d.Decrypt(filepath.Join(i.Config, "secrets", "backup.sops.env")); err == nil {
		environment = secrets.Dotenv(decrypted)
	}
	if environment["RESTIC_REPOSITORY"] == "" {
		environment["RESTIC_REPOSITORY"] = location
	}
	if environment["RESTIC_REPOSITORY"] == "" {
		return nil, false, nil
	}
	machine, err := backup.MachineID(d.MachineIDFile, i.Data)
	if err != nil {
		return nil, true, err
	}
	return &backup.Backups{
		Installation: i,
		Repository:   resticFor(d.Run(io.Discard, io.Discard), environment, io.Discard),
		MachineID:    machine,
		Report:       report.New(io.Discard, io.Discard, nil),
	}, true, nil
}

func (d Dependencies) unitDir() string {
	if d.UnitDir != "" {
		return d.UnitDir
	}
	return filepath.Join(d.Home, ".config", "systemd", "user")
}
