package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"

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
	latest := "the latest " + semver.Major(current) + " release"
	if force {
		latest = "the latest release"
	}
	_, _ = fmt.Fprintf(p.w, "Current version: %s\nChecking for updates to %s...\n", current, latest)
}

func (p printedProgress) Updating(target string) {
	_, _ = fmt.Fprintf(p.w, "Updating to %s...\n", target)
}

func describe(result selfupdate.Result) string {
	var message strings.Builder
	if result.To != "" {
		fmt.Fprintf(&message, "Successfully updated from %s to %s\n", result.From, result.To)
	} else {
		fmt.Fprintf(&message, "mse is up to date (%s)\n", result.From)
	}
	if result.NewerMajor != nil {
		fmt.Fprintf(&message, "%s is available and may need config changes: mse update --force (release notes: %s)\n", result.NewerMajor.Tag, result.NewerMajor.URL)
	}
	return message.String()
}
