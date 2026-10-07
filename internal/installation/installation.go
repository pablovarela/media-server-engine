package installation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	ErrNoInstallation       = errors.New("no installation")
	ErrSeveralInstallations = errors.New("several installations")
)

type Bases struct {
	Config string
	Data   string
	State  string
	Cache  string
}

type Installation struct {
	Name     string
	Config   string
	Data     string
	State    string
	Settings map[string]string
}

func BasesFrom(getenv func(string) string, home string) Bases {
	base := func(variable, fallback string) string {
		if value := getenv(variable); value != "" {
			return value
		}
		return filepath.Join(home, fallback)
	}
	return Bases{
		Config: base("XDG_CONFIG_HOME", ".config"),
		Data:   base("XDG_DATA_HOME", filepath.Join(".local", "share")),
		State:  base("XDG_STATE_HOME", filepath.Join(".local", "state")),
		Cache:  base("XDG_CACHE_HOME", ".cache"),
	}
}

func Load(bases Bases, lookup func(string) string) (*Installation, error) {
	root := filepath.Join(bases.Config, "mse")
	names, err := installationsIn(root)
	if err != nil {
		return nil, err
	}
	name, err := choose(root, names)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(root, name, "installation.env")
	text, err := os.ReadFile(path) //nolint:gosec // reads the installation's own installation.env
	if err != nil {
		return nil, err
	}
	settings := ParseEnv(string(text), lookup)
	switch settings["INSTALLATION_NAME"] {
	case "":
		return nil, fmt.Errorf("INSTALLATION_NAME is not set in %s", path)
	case name:
	default:
		return nil, fmt.Errorf("INSTALLATION_NAME is %s in %s, not %s", settings["INSTALLATION_NAME"], path, name)
	}
	return &Installation{
		Name:     name,
		Config:   filepath.Join(root, name),
		Data:     filepath.Join(bases.Data, "mse", name),
		State:    filepath.Join(bases.State, "mse", name),
		Settings: settings,
	}, nil
}

func Names(bases Bases) ([]string, error) {
	return installationsIn(filepath.Join(bases.Config, "mse"))
}

func installationsIn(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if _, err := os.Stat(filepath.Join(root, entry.Name(), "installation.env")); err == nil {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func choose(root string, names []string) (string, error) {
	switch {
	case len(names) == 1:
		return names[0], nil
	case len(names) == 0:
		return "", fmt.Errorf("%w in %s", ErrNoInstallation, root)
	default:
		return "", fmt.Errorf("%w in %s: a machine runs one installation; remove the ones it shouldn't have:\n%s", ErrSeveralInstallations, root, strings.TrimRight(listed(names), "\n"))
	}
}

func listed(names []string) string {
	var lines strings.Builder
	for _, name := range names {
		lines.WriteString("  " + name + "\n")
	}
	return lines.String()
}

func (i *Installation) Role() string {
	if _, err := os.Stat(filepath.Join(i.Data, ".backup-main")); err == nil {
		return "main"
	}
	return "secondary"
}

func (i *Installation) HomepagePort() string {
	if port := i.Settings["HOMEPAGE_PORT"]; port != "" {
		return port
	}
	return "80"
}

func (i *Installation) HomepageAllowedHosts(network string) string {
	suffix := ""
	if port := i.HomepagePort(); port != "80" {
		suffix = ":" + port
	}
	hosts := append([]string{network, "localhost", "127.0.0.1"}, strings.Fields(strings.ReplaceAll(i.Settings["HOMEPAGE_ALLOWED_HOSTS"], ",", " "))...)
	for n := range hosts {
		hosts[n] += suffix
	}
	return strings.Join(hosts, ",")
}
