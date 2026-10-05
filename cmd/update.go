package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/paint"
	"github.com/pablovarela/media-server-engine/internal/selfupdate"
	"github.com/pablovarela/media-server-engine/internal/version"
)

type updater interface {
	Update(ctx context.Context, current version.Build, force bool, progress selfupdate.Progress) (selfupdate.Result, error)
}

func newUpdateCommand(build version.Build, update updater) *cobra.Command {
	var force bool
	command := &cobra.Command{
		Use:   "update",
		Short: "Update mse to the newest release of its major version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := update.Update(cmd.Context(), build, force, printedProgress{w: cmd.OutOrStdout()})
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), describe(result))
			return err
		},
	}
	command.Flags().BoolVar(&force, "force", false, "update to a newer major version too, which may need config changes")
	return command
}

type printedProgress struct {
	w io.Writer
}

func (p printedProgress) Checking(current string, force bool) {
	checking := "a newer compatible release"
	if force {
		checking = "a newer release, including ones that need config changes"
	}
	_, _ = fmt.Fprintf(p.w, "Current version: %s\nChecking for %s...\n", paint.Bold(current), checking)
}

func (p printedProgress) Updating(target string) {
	_, _ = fmt.Fprintf(p.w, "Updating to %s...\n", paint.Bold(target))
}

func describe(result selfupdate.Result) string {
	var message strings.Builder
	if result.To != "" {
		message.WriteString(paint.Success(fmt.Sprintf("Successfully updated from %s to %s", result.From, result.To)) + "\n")
	} else {
		message.WriteString(paint.Success(fmt.Sprintf("mse is up to date (%s)", result.From)) + "\n")
	}
	if result.NewerMajor != nil {
		message.WriteString(paint.Warning(fmt.Sprintf("%s is available and needs config changes", paint.Bold(result.NewerMajor.Tag))))
		fmt.Fprintf(&message, " (release notes: %s)\nInstall it with: mse update --force\n", result.NewerMajor.URL)
	}
	return message.String()
}
