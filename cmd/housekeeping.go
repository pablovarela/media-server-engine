package cmd

import (
	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/downloads"
)

func newRemoveExecutableDownloadsCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "clean-downloads",
		Short: "Remove and blocklist downloads Sonarr and Radarr flag as executable",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			i, err := deps.installation(cmd)
			if err != nil {
				return err
			}
			return downloads.Clean(cmd.Context(), deps.HTTP, i, 200, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
}
