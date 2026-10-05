package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/report"
	"github.com/pablovarela/media-server-engine/internal/secrets"
)

const noLogin = "no login on the local network"

func newLoginsCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "logins",
		Short: "Print the users and passwords of the apps",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			i, err := deps.installation(cmd)
			if err != nil {
				return err
			}
			keys, err := secrets.Apps(i, deps.Decrypt)
			if err != nil {
				return err
			}
			var out strings.Builder
			fmt.Fprintf(&out, "%-12s %-10s Password\n", "App", "User")
			fmt.Fprintf(&out, "%-12s %-10s %s\n", "Jellyfin", i.Settings["JELLYFIN_ADMIN_USER"], keys["JELLYFIN_ADMIN_PASSWORD"])
			fmt.Fprintf(&out, "%-12s sign in with the Jellyfin account\n", "Seerr")
			fmt.Fprintf(&out, "%-12s %-10s %s\n", "Deluge", "", keys["DELUGE_WEB_PASSWORD"])
			fmt.Fprintf(&out, "%-12s %-10s %s\n", "Portainer", "admin", keys["PORTAINER_ADMIN_PASSWORD"])
			for _, app := range []string{"Sonarr", "Radarr", "Prowlarr"} {
				fmt.Fprintf(&out, "%-12s %s\n", app, noLogin)
			}
			_, err = fmt.Fprint(report.From(cmd.Context()).Data(), out.String())
			return err
		},
	}
}
