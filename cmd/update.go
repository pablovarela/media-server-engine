package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/selfupdate"
	"github.com/pablovarela/media-server-engine/internal/version"
)

type updater interface {
	Update(ctx context.Context, current version.Build, force bool) (selfupdate.Result, error)
}

func newUpdateCommand(build version.Build, update updater) *cobra.Command {
	var force bool
	command := &cobra.Command{
		Use:   "update",
		Short: "Update mse to the newest release of its major version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := update.Update(cmd.Context(), build, force)
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

func describe(result selfupdate.Result) string {
	var message strings.Builder
	if result.To != "" {
		fmt.Fprintf(&message, "updated mse from %s to %s\n", result.From, result.To)
	} else {
		fmt.Fprintf(&message, "mse %s is up to date\n", result.From)
	}
	if result.NewerMajor != nil {
		fmt.Fprintf(&message, "%s is available and may need config changes: mse update --force (release notes: %s)\n", result.NewerMajor.Tag, result.NewerMajor.URL)
	}
	return message.String()
}
