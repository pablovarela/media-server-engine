package cmd

import (
	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/downloads"
	"github.com/pablovarela/media-server-engine/internal/images"
)

func newPruneStackImagesCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "prune-stack-images",
		Short: "Remove local images of the stack that the config no longer pins",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			i, err := deps.installation(cmd)
			if err != nil {
				return err
			}
			docker, err := deps.Images()
			if err != nil {
				return err
			}
			return images.Prune(cmd.Context(), docker, i, cmd.OutOrStdout())
		},
	}
}

func newRemoveExecutableDownloadsCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "remove-executable-downloads",
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
