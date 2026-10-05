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

var (
	Stack      = Kind{Name: "media-server", EngineFile: "docker-compose.yml", ImagesFile: "images.yml", Override: true}
	Monitoring = Kind{Name: "monitoring", EngineFile: "docker-compose.monitoring.yml", ImagesFile: "images.monitoring.yml"}
)

var (
	composeFiles  = []string{"docker-compose.yml", "docker-compose.monitoring.yml"}
	engineFolders = []string{"grafana", "prometheus"}
	mountedEmpty  = []string{".homepage", ".homepage-images"}
)

func Prepare(engine fs.FS, state string) error {
	for _, dir := range mountedEmpty {
		if err := os.MkdirAll(filepath.Join(state, dir), 0o755); err != nil { //nolint:gosec // containers running as other users read these folders
			return err
		}
	}
	for _, file := range composeFiles {
		if err := copyEngineFile(engine, file, state); err != nil {
			return err
		}
	}
	for _, folder := range engineFolders {
		if err := copyEngineFolder(engine, folder, state); err != nil {
			return err
		}
	}
	return nil
}

func copyEngineFile(engine fs.FS, path, state string) error {
	content, err := fs.ReadFile(engine, path)
	if err != nil {
		return err
	}
	return writeIfChanged(filepath.Join(state, filepath.FromSlash(path)), content)
}

func copyEngineFolder(engine fs.FS, folder, state string) error {
	wanted := map[string]bool{}
	err := fs.WalkDir(engine, folder, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		relative, _ := strings.CutPrefix(path, folder+"/")
		wanted[relative] = true
		return copyEngineFile(engine, path, state)
	})
	if err != nil {
		return err
	}
	return removeUnwanted(filepath.Join(state, folder), wanted)
}

func removeUnwanted(dir string, wanted map[string]bool) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || wanted[path] {
			return err
		}
		return root.Remove(path)
	})
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
