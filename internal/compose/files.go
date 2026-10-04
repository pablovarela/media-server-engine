package compose

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"go.yaml.in/yaml/v3"

	"github.com/pablovarela/media-server-engine/internal/installation"
)

type Kind struct {
	Name       string
	EngineFile string
	ImagesFile string
	Override   bool
}

var (
	Stack      = Kind{Name: "media-server", EngineFile: "docker-compose.yml", ImagesFile: "images.yml", Override: true}
	Monitoring = Kind{Name: "monitoring", EngineFile: "docker-compose.monitoring.yml", ImagesFile: "images.monitoring.yml"}
)

var engineEntries = []string{"docker-compose.yml", "docker-compose.monitoring.yml", "grafana", "prometheus"}

func Prepare(engine fs.FS, state string) error {
	for _, entry := range engineEntries {
		err := fs.WalkDir(engine, entry, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			content, err := fs.ReadFile(engine, path)
			if err != nil {
				return err
			}
			return writeIfChanged(filepath.Join(state, filepath.FromSlash(path)), content)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func writeIfChanged(path string, content []byte) error {
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, content) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // containers running as other users read these folders
		return err
	}
	return os.WriteFile(path, content, 0o644) //nolint:gosec // compose and the containers read these files
}

func Variables(i *installation.Installation, network string, dockerGID int) []string {
	tz := i.Settings["TZ"]
	if tz == "" {
		tz = "Etc/UTC"
	}
	return []string{
		"DATA_DIR=" + i.Data,
		"CONFIG_DIR=" + i.Config,
		"TZ=" + tz,
		"HOMEPAGE_PORT=" + i.HomepagePort(),
		"HOMEPAGE_ALLOWED_HOSTS=" + i.HomepageAllowedHosts(network),
		"DOCKER_GID=" + strconv.Itoa(dockerGID),
		"COMPOSE_PROFILES=",
	}
}

func DockerGID() int {
	if runtime.GOOS != "linux" {
		return 0
	}
	info, err := os.Stat("/var/run/docker.sock")
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return 0
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(stat.Gid)
	}
	return 0
}

func Profiles(i *installation.Installation, kind Kind, wiring bool) ([]string, error) {
	var profiles []string
	if kind.Name == Stack.Name {
		pinned, err := homepagePinned(filepath.Join(i.Config, kind.ImagesFile))
		if err != nil {
			return nil, err
		}
		if pinned {
			profiles = append(profiles, "homepage")
		}
	}
	if wiring {
		profiles = append(profiles, "wiring")
	}
	return profiles, nil
}

func homepagePinned(path string) (bool, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var images struct {
		Services map[string]struct {
			Image string `yaml:"image"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(text, &images); err != nil {
		return false, err
	}
	return strings.TrimSpace(images.Services["homepage"].Image) != "", nil
}
