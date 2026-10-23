package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/downloads"
	"github.com/pablovarela/media-server-engine/internal/jobs"
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
			pings := deps.pinger(cmd, i)
			finishing := context.WithoutCancel(cmd.Context())
			pings.Ping(cmd.Context(), jobs.CleanDownloads, "/start")
			err = downloads.Clean(cmd.Context(), deps.HTTP, i, 200, cmd.OutOrStdout(), cmd.ErrOrStderr())
			defer shieldSignals()()
			if err != nil {
				pings.Ping(finishing, jobs.CleanDownloads, "/fail")
				return err
			}
			pings.Ping(finishing, jobs.CleanDownloads, "")
			return nil
		},
	}
}
