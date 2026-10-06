package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/report"
)

var apps = []struct {
	name string
	port int
}{
	{"Jellyfin", 8096}, {"Seerr", 5055}, {"Sonarr", 8989}, {"Radarr", 7878}, {"Prowlarr", 9696},
	{"Bazarr", 6767}, {"Deluge", 8112}, {"Maintainerr", 6246}, {"Portainer", 9000},
}

func newURLsCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "urls",
		Short: "Print the address of every app",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			i, err := deps.installation(cmd)
			if err != nil {
				return err
			}
			host, err := i.NetworkName(deps.Host)
			if err != nil {
				return err
			}
			pinned, err := compose.HomepagePinned(i)
			if err != nil {
				return err
			}
			var out strings.Builder
			if pinned {
				fmt.Fprintf(&out, "%-12s %s\n", "Home", homepageAddress(i, host))
			}
			for _, app := range apps {
				fmt.Fprintf(&out, "%-12s http://%s:%d\n", app.name, host, app.port)
			}
			_, err = fmt.Fprint(report.From(cmd.Context()).Data(), out.String())
			return err
		},
	}
}

func homepageAddress(i *installation.Installation, host string) string {
	address := "http://" + host
	if port := i.HomepagePort(); port != "80" {
		address += ":" + port
	}
	return address
}
