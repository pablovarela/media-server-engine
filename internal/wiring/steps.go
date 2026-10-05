package wiring

import (
	"io"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
)

const arrPing = "/ping"

var HealthChecks = map[string]Health{
	sonarrKind:     {Variable: "SONARR_URL", Fallback: "http://localhost:8989", Path: arrPing},
	radarrKind:     {Variable: "RADARR_URL", Fallback: "http://localhost:7878", Path: arrPing},
	prowlarrApp:    {Variable: "PROWLARR_URL", Fallback: "http://localhost:9696", Path: arrPing},
	jellyfinApp:    {Variable: "JELLYFIN_URL", Fallback: "http://localhost:8096", Path: "/System/Info/Public"},
	delugeApp:      {Variable: "DELUGE_URL", Fallback: "http://localhost:8112", Path: "/"},
	seerrApp:       {Variable: "SEERR_URL", Fallback: "http://localhost:5055", Path: "/api/v1/status"},
	bazarrApp:      {Variable: "BAZARR_URL", Fallback: "http://localhost:6767", Path: "/api/system/ping"},
	maintainerrApp: {Variable: "MAINTAINERR_URL", Fallback: "http://localhost:6246", Path: "/api/settings/version"},
}

func Steps(docker Docker, configarr OneOff, tool io.Writer) []Step {
	return []Step{
		{Name: prowlarrApp, Apps: []string{prowlarrApp}, Run: Prowlarr},
		{Name: jellyfinApp, Apps: []string{jellyfinApp}, Run: Jellyfin},
		{Name: "library-updates", Apps: []string{sonarrKind, radarrKind}, Run: LibraryUpdates},
		{Name: delugeApp, Apps: []string{delugeApp}, Run: Deluge(docker)},
		{Name: "configarr", Run: Configarr(configarr, tool)},
		{Name: seerrApp, Apps: []string{seerrApp}, Run: Seerr},
		{Name: bazarrApp, Apps: []string{bazarrApp}, Run: Bazarr},
		{Name: maintainerrApp, Apps: []string{maintainerrApp}, Run: Maintainerr},
		{Name: "prowlarr-sync", Apps: []string{prowlarrApp, sonarrKind, radarrKind}, Run: ProwlarrSync},
	}
}

func NewDocker() (Docker, error) {
	dockerCLI, err := command.NewDockerCli()
	if err != nil {
		return nil, err
	}
	if err := dockerCLI.Initialize(&flags.ClientOptions{}); err != nil {
		return nil, err
	}
	return DockerClient{API: dockerCLI.Client()}, nil
}
