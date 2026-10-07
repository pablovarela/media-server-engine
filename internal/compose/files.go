package compose

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"go.yaml.in/yaml/v3"

	"github.com/pablovarela/media-server-engine/internal/files"
	"github.com/pablovarela/media-server-engine/internal/installation"
)

type Kind struct {
	Name       string
	EngineFile string
	ImagesFile string
	Override   bool
}

var Stack = Kind{Name: "media-server", EngineFile: "docker-compose.yml", ImagesFile: "images.yml", Override: true}

var mountedEmpty = []string{".homepage", ".homepage-images"}

func Prepare(engine fs.FS, state string) error {
	for _, dir := range mountedEmpty {
		if err := os.MkdirAll(filepath.Join(state, dir), 0o755); err != nil { //nolint:gosec // containers running as other users read these folders
			return err
		}
	}
	return copyEngineFile(engine, Stack.EngineFile, state)
}

func copyEngineFile(engine fs.FS, path, state string) error {
	content, err := fs.ReadFile(engine, path)
	if err != nil {
		return err
	}
	return writeIfChanged(filepath.Join(state, filepath.FromSlash(path)), content)
}

func writeIfChanged(path string, content []byte) error {
	_, err := files.WriteIfChanged(path, content, 0o644)
	return err
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
		pinned, err := HomepagePinned(i)
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

func HomepagePinned(i *installation.Installation) (bool, error) {
	text, err := os.ReadFile(filepath.Join(i.Config, Stack.ImagesFile))
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
