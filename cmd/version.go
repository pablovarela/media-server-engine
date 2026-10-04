package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/version"
)

func newVersionCommand(build version.Build) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of mse",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), build)
			return err
		},
	}
}
