package homepage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/pablovarela/media-server-engine/internal/files"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/secrets"
)

func readFile(path string) []byte {
	text, _ := os.ReadFile(path) //nolint:gosec // reads the installation's own state and app files
	return text
}

func HealthchecksKey(i *installation.Installation) string {
	return secrets.Dotenv(readFile(filepath.Join(i.State, ".secrets", "healthchecks.env")))["HEALTHCHECKS_API_KEY"]
}

func Env(i *installation.Installation) string {
	apps := secrets.Dotenv(readFile(filepath.Join(i.State, ".secrets", "apps.env")))
	wiring := func(name string) string {
		return strings.TrimSpace(string(readFile(filepath.Join(i.Data, "volumes", ".wiring", name))))
	}
	values := [][2]string{
		{"SONARR_KEY", apps["SONARR_API_KEY"]},
		{"RADARR_KEY", apps["RADARR_API_KEY"]},
		{"PROWLARR_KEY", apps["PROWLARR_API_KEY"]},
		{"DELUGE_PASSWORD", apps["DELUGE_WEB_PASSWORD"]},
		{"JELLYFIN_KEY", wiring("jellyfin.key")},
		{"SEERR_KEY", seerrKey(i)},
		{"BAZARR_KEY", bazarrKey(i)},
		{"GLUETUN_KEY", wiring("gluetun-control.key")},
		{"HEALTHCHECKS_KEY", HealthchecksKey(i)},
	}
	var env strings.Builder
	for _, v := range values {
		fmt.Fprintf(&env, "HOMEPAGE_VAR_%s=%s\n", v[0], v[1])
	}
	return env.String()
}

func seerrKey(i *installation.Installation) string {
	var settings struct {
		Main struct {
			APIKey string `json:"apiKey"`
		} `json:"main"`
	}
	_ = json.Unmarshal(readFile(filepath.Join(i.Data, "volumes", "seerr", "config", "settings.json")), &settings)
	return settings.Main.APIKey
}

func bazarrKey(i *installation.Installation) string {
	var config struct {
		Auth struct {
			APIKey string `yaml:"apikey"`
		} `yaml:"auth"`
	}
	_ = yaml.Unmarshal(readFile(filepath.Join(i.Data, "volumes", "bazarr", "config", "config", "config.yaml")), &config)
	return config.Auth.APIKey
}

func WriteEnv(i *installation.Installation, text string) (bool, error) {
	return files.WriteIfChanged(filepath.Join(i.State, ".secrets", "homepage.env"), []byte(text), 0o600)
}
