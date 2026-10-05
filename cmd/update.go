package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/backup"
	"github.com/pablovarela/media-server-engine/internal/gitconfig"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/paint"
	"github.com/pablovarela/media-server-engine/internal/report"
	"github.com/pablovarela/media-server-engine/internal/selfupdate"
	"github.com/pablovarela/media-server-engine/internal/version"
)

type updater interface {
	Update(ctx context.Context, current version.Build, force bool, progress selfupdate.Progress) (selfupdate.Result, error)
}

var errHandedOver = errors.New("handed over to the updated mse")

func newUpdateCommand(deps Dependencies) *cobra.Command {
	var force, applying bool
	command := &cobra.Command{
		Use:     "update",
		Short:   "Update the config and mse to the newest release of its major version",
		Example: "  mse update --apply    update, then apply the config with the updated mse (what the nightly timer runs)",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if applying {
				return deps.updateAndApply(cmd, force)
			}
			_, err := deps.update(cmd, force)
			return err
		},
	}
	command.Flags().BoolVar(&force, "force", false, "update to a newer major version too, which may need config changes")
	command.Flags().BoolVar(&applying, "apply", false, "then apply the config with the updated mse, reporting to Healthchecks")
	return command
}

func (d Dependencies) update(cmd *cobra.Command, force bool) (selfupdate.Result, error) {
	i, err := d.anyInstallation(cmd)
	switch {
	case errors.Is(err, installation.ErrNoInstallation):
		report.From(cmd.Context()).Say("No installation here; updating mse only.")
	case errors.Is(err, installation.ErrSeveralInstallations):
		report.From(cmd.Context()).Say("Several installations here; updating mse only. Choose one with --installation <name> to update its config too.")
	case err != nil:
		return selfupdate.Result{}, err
	default:
		if err := d.fetchConfig(cmd, i); err != nil {
			return selfupdate.Result{}, err
		}
	}
	return d.updateBinary(cmd, force)
}

func (d Dependencies) updateBinary(cmd *cobra.Command, force bool) (selfupdate.Result, error) {
	result, err := d.Update.Update(cmd.Context(), d.Build, force, printedProgress{w: cmd.OutOrStdout()})
	if errors.Is(err, selfupdate.ErrDevBuild) {
		report.From(cmd.Context()).Step("Updating mse").Done("dev build, not updated")
		return selfupdate.Result{From: d.Build.Version}, nil
	}
	if err != nil {
		return result, err
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), describe(result))
	return result, err
}

func (d Dependencies) fetchConfig(cmd *cobra.Command, i *installation.Installation) error {
	ctx := cmd.Context()
	tool := report.From(ctx).Tool("git")
	repository := gitconfig.Repository{Runner: d.Run(tool, tool), Dir: i.Config}
	changes, err := repository.Changes(ctx)
	if err != nil {
		return err
	}
	remote, err := repository.HasRemote(ctx)
	if err != nil {
		return err
	}
	if changes != "" {
		return uncommitted(i.Config, changes, remote)
	}
	step := report.From(ctx).Step("Updating the config")
	if !remote {
		step.Done("no remote, not pulled")
		return nil
	}
	taken, err := repository.FastForward(ctx)
	if err != nil {
		return step.Fail(err)
	}
	step.Done(commitsTaken(taken))
	return nil
}

func commitsTaken(n int) string {
	switch n {
	case 0:
		return "up to date"
	case 1:
		return "took 1 commit"
	}
	return fmt.Sprintf("took %d commits", n)
}

func uncommitted(config, changes string, remote bool) error {
	lines := []string{"The config has changes that are not committed:", changes, "See them with: git -C " + config + " diff"}
	if remote {
		lines = append(lines, "Commit and push them, then update again:", fmt.Sprintf(`  git -C %s commit -am "<what changed>" && git -C %s push`, config, config))
	} else {
		lines = append(lines, "Commit them, then update again:", fmt.Sprintf(`  git -C %s commit -am "<what changed>"`, config))
	}
	return errors.New(strings.Join(lines, "\n"))
}

func (d Dependencies) updateAndApply(cmd *cobra.Command, force bool) error {
	i, err := d.anyInstallation(cmd)
	if err != nil {
		return err
	}
	d.pinger(cmd, i).Ping(cmd.Context(), "update", "/start")
	err = d.reported(cmd, i, func() error {
		if err := d.waitForBackup(cmd, i); err != nil {
			return err
		}
		result, err := d.update(cmd, force)
		if err != nil {
			return err
		}
		if result.To != "" {
			return d.handOver(cmd, result.Path, i)
		}
		return d.apply(cmd)
	})
	if errors.Is(err, errHandedOver) {
		return nil
	}
	return err
}

func (d Dependencies) handOver(cmd *cobra.Command, path string, i *installation.Installation) error {
	args := []string{path, "apply", "--after-update=" + runIDOf(cmd.Context()), "--installation", i.Name}
	if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
		args = append(args, "--verbose")
	}
	report.From(cmd.Context()).Say("Applying the config with the updated mse...")
	if err := d.Exec(path, args); err != nil {
		return fmt.Errorf("could not run the updated mse at %s: %w", path, err)
	}
	return errHandedOver
}

func (d Dependencies) waitForBackup(cmd *cobra.Command, i *installation.Installation) error {
	r := report.From(cmd.Context())
	return backup.WaitWhileRunning(cmd.Context(), backup.LockPath(i.Data), backup.Waiting{
		Timeout: time.Hour, Poll: 10 * time.Second, Now: d.Now, Sleep: d.Pause,
		Announce: func() { r.Say("Waiting for the running backup to finish...") },
	})
}

type printedProgress struct {
	w io.Writer
}

func (p printedProgress) Checking(current string, force bool) {
	checking := "a newer compatible release"
	if force {
		checking = "a newer release, including ones that need config changes"
	}
	_, _ = fmt.Fprintf(p.w, "Current version: %s\nChecking for %s...\n", paint.Stdout.Bold(current), checking)
}

func (p printedProgress) Updating(target string) {
	_, _ = fmt.Fprintf(p.w, "Updating to %s...\n", paint.Stdout.Bold(target))
}

func describe(result selfupdate.Result) string {
	var message strings.Builder
	if result.To != "" {
		message.WriteString(paint.Stdout.Success(fmt.Sprintf("Successfully updated from %s to %s", result.From, result.To)) + "\n")
	} else {
		message.WriteString(paint.Stdout.Success(fmt.Sprintf("mse is up to date (%s)", result.From)) + "\n")
	}
	if result.NewerMajor != nil {
		message.WriteString(paint.Stdout.Warning(fmt.Sprintf("%s is available and needs config changes", paint.Stdout.Bold(result.NewerMajor.Tag))))
		fmt.Fprintf(&message, " (release notes: %s)\nInstall it with: mse update --force\n", result.NewerMajor.URL)
	}
	return message.String()
}
