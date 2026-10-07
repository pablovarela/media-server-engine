package cmd

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/downloads"
	"github.com/pablovarela/media-server-engine/internal/timers"
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
			record := deps.recorder(i)
			if err := record.Start(timers.Cleanup.Name); err != nil {
				return err
			}
			cleaned := downloads.Clean(cmd.Context(), deps.HTTP, i, 200, cmd.OutOrStdout(), cmd.ErrOrStderr())
			return errors.Join(cleaned, record.Finish(timers.Cleanup.Name, cleaned != nil))
		},
	}
}
