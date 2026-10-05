package homepage

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/pablovarela/media-server-engine/internal/files"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/version"
)

const repositoryURL = "https://github.com/pablovarela/media-server-engine"

var pageFiles = []string{"settings.yaml", "services.yaml", "widgets.yaml", "bookmarks.yaml", "custom.css"}

type Inputs struct {
	Network         string
	Version         string
	EngineURL       string
	Role            string
	ShortHost       string
	HealthchecksKey string
}

func EngineURL(b version.Build) string {
	if semver.IsValid(b.Version) {
		return repositoryURL + "/releases/tag/" + b.Version
	}
	if commit := strings.TrimSuffix(b.Commit, "-dirty"); commit != "" {
		return repositoryURL + "/commit/" + commit
	}
	return ""
}

func Draw(ctx context.Context, engine fs.FS, i *installation.Installation, in Inputs, checks CheckLister, warn io.Writer) (bool, error) {
	out := filepath.Join(i.State, ".homepage")
	for _, name := range pageFiles {
		text, err := pageFile(engine, i, name)
		if err != nil {
			return false, err
		}
		text = strings.NewReplacer(
			"@INSTALLATION_NAME@", i.Name,
			"@ENGINE_VERSION@", in.Version,
			"@HOST@", in.Network,
			"@ENGINE_URL@", in.EngineURL,
		).Replace(text)
		switch name {
		case "services.yaml":
			text, err = renderServices(ctx, text, slugFor(i.Name, in), in.HealthchecksKey, checks, warn)
		case "widgets.yaml":
			text, err = renderWidgets(text)
		}
		if err != nil {
			return false, err
		}
		if _, err := files.WriteIfChanged(filepath.Join(out, name), []byte(text), 0o644); err != nil {
			return false, err
		}
	}
	return syncImages(i)
}

func pageFile(engine fs.FS, i *installation.Installation, name string) (string, error) {
	text, err := os.ReadFile(filepath.Join(i.Config, "homepage", name)) //nolint:gosec // reads the installation's own page files
	if err == nil {
		return string(text), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	text, err = fs.ReadFile(engine, "homepage/"+name)
	return string(text), err
}

func slugFor(name string, in Inputs) func(job string) string {
	return func(job string) string {
		slug := name + "-" + strings.ToLower(job)
		if job == "UPDATE" && in.Role != "main" {
			slug += "-" + in.ShortHost
		}
		return slug
	}
}
