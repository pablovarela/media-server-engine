package cmd

import (
	"context"
	"errors"
	"io"

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
			if healthchecksKey(i, "HEALTHCHECKS_PING_KEY") == "" {
				pings.pings.ErrOut = io.Discard
			}
			finishing := context.WithoutCancel(cmd.Context())
			pings.Ping(cmd.Context(), jobs.CleanDownloads, "/start")
			err = downloads.Clean(cmd.Context(), deps.HTTP, i, 200, cmd.OutOrStdout(), cmd.ErrOrStderr())
			defer shieldSignals()()
			switch {
			case errors.Is(err, downloads.ErrNotRead):
				// Without a success ping healthchecks.io calls the check down only once the queue stays unreadable past its grace.
				pings.runs.finish(jobs.CleanDownloads, false)
				return nil
			case err != nil:
				pings.Ping(finishing, jobs.CleanDownloads, "/fail")
				return err
			}
			pings.Ping(finishing, jobs.CleanDownloads, "")
			return nil
		},
	}
}
