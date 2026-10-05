package cmd

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"

	"github.com/pablovarela/media-server-engine/internal/apply"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/paint"
	"github.com/pablovarela/media-server-engine/internal/process"
	"github.com/pablovarela/media-server-engine/internal/report"
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

func (t *appliedTimers) Set(ctx context.Context) (string, error) {
	if !semver.IsValid(t.d.Build.Version) {
		return "skipped: a dev build can't be updated by mse update", nil
	}
	account, _, err := t.d.Account()
	if err != nil {
		return "", err
	}
	if !t.lingering(ctx, account) {
		return "skipped: they need lingering, once: sudo loginctl enable-linger " + account, nil
	}
	if t.d.Environment("SOPS_AGE_KEY") != "" {
		return "skipped: SOPS_AGE_KEY can't reach the timers; keep the key in a file and set SOPS_AGE_KEY_FILE", nil
	}
	values, err := t.values()
	if err != nil {
		return "", err
	}
	t.warnWithoutToken(ctx, account, values)
	installer := timers.Installer{Runner: t.d.Run(io.Discard, io.Discard), Dir: t.d.unitDir()}
	outcome, err := installer.Install(ctx, t.role(ctx), values)
	if err != nil {
		return "", err
	}
	return outcome.String(), nil
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

func (t *appliedTimers) warnWithoutToken(ctx context.Context, account string, values timers.Values) {
	args := append([]string{"-i", "HOME=" + t.d.Home, "USER=" + account, "PATH=" + systemdPath}, values.Environment...)
	result, err := t.d.Run(io.Discard, io.Discard).Output(ctx, process.Command{Name: "env", Args: append(args, "gh", "auth", "token")})
	if err != nil || result.Exit != 0 || strings.TrimSpace(string(result.Stdout)) == "" {
		report.From(ctx).Warn(paint.Stderr.Warning(noUnattendedToken))
	}
}

func (t *appliedTimers) role(ctx context.Context) timers.Role {
	b, err := t.d.backups(t.cmd, telling)
	if err == nil {
		var runs bool
		if runs, err = b.RunsBackups(ctx); err == nil && runs {
			return timers.Main
		}
		if err == nil {
			return timers.Secondary
		}
	}
	report.From(ctx).Warn(paint.Stderr.Warning(fmt.Sprintf("could not tell whether this machine is the main (%v); the backup timers are left as they are", err)))
	return timers.Unknown
}

func (d Dependencies) unitDir() string {
	if d.UnitDir != "" {
		return d.UnitDir
	}
	return filepath.Join(d.Home, ".config", "systemd", "user")
}
