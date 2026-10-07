package images

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"go.yaml.in/yaml/v3"

	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/report"
)

var imageFiles = []string{"images.yml", "compose.override.yml"}

type Client interface {
	ImageList(ctx context.Context, options client.ImageListOptions) (client.ImageListResult, error)
	ImageRemove(ctx context.Context, image string, options client.ImageRemoveOptions) (client.ImageRemoveResult, error)
}

func NewDocker() (Client, error) {
	dockerCLI, err := command.NewDockerCli()
	if err != nil {
		return nil, err
	}
	if err := dockerCLI.Initialize(&flags.ClientOptions{}); err != nil {
		return nil, err
	}
	return dockerCLI.Client(), nil
}

func untagged(name string) string {
	if tag := strings.LastIndex(name, ":"); tag > strings.LastIndex(name, "/") {
		return name[:tag]
	}
	return name
}

func Pinned(i *installation.Installation) map[string]bool {
	pinned := map[string]bool{}
	for _, file := range imageFiles {
		text, err := os.ReadFile(filepath.Join(i.Config, file)) //nolint:gosec // reads the installation's own image pins
		if err != nil {
			continue
		}
		var declared struct {
			Services map[string]struct {
				Image string `yaml:"image"`
			} `yaml:"services"`
		}
		if yaml.Unmarshal(text, &declared) != nil {
			continue
		}
		for _, service := range declared.Services {
			if name, digest, found := strings.Cut(service.Image, "@"); found && strings.HasPrefix(digest, "sha256:") {
				pinned[untagged(name)+"@"+digest] = true
			}
		}
	}
	return pinned
}

func Outdated(pinned map[string]bool, local []image.Summary) []string {
	repositories := map[string]bool{}
	for reference := range pinned {
		repository, _, _ := strings.Cut(reference, "@")
		repositories[repository] = true
	}
	found := map[string]bool{}
	for _, summary := range local {
		for _, reference := range unpinnedReferences(summary, repositories, pinned) {
			found[reference] = true
		}
	}
	outdated := slices.Collect(maps.Keys(found))
	slices.Sort(outdated)
	return outdated
}

func unpinnedReferences(summary image.Summary, repositories, pinned map[string]bool) []string {
	digests := map[string]string{}
	for _, repoDigest := range summary.RepoDigests {
		repository, digest, _ := strings.Cut(repoDigest, "@")
		digests[repository] = digest
	}
	var references []string
	tagged := map[string]bool{}
	for _, repoTag := range summary.RepoTags {
		repository := untagged(repoTag)
		tagged[repository] = true
		if repositories[repository] && !pinned[repository+"@"+digests[repository]] {
			references = append(references, repoTag)
		}
	}
	for repository, digest := range digests {
		if !tagged[repository] && repositories[repository] && !pinned[repository+"@"+digest] {
			references = append(references, repository+"@"+digest)
		}
	}
	return references
}

func Prune(ctx context.Context, c Client, i *installation.Installation, r *report.Reporter) error {
	pinned := Pinned(i)
	step := r.Step("Removing outdated images")
	listed, err := c.ImageList(ctx, client.ImageListOptions{All: true})
	if err != nil {
		return step.Fail(err)
	}
	tool := r.Tool("docker")
	removed, kept := 0, 0
	for _, reference := range Outdated(pinned, listed.Items) {
		if _, err := c.ImageRemove(ctx, reference, client.ImageRemoveOptions{}); err != nil {
			kept++
			_, _ = fmt.Fprintf(tool, "kept %s (still in use)\n", reference)
			continue
		}
		removed++
		_, _ = fmt.Fprintf(tool, "removed %s\n", reference)
	}
	step.Done(result(removed, kept, len(pinned)))
	return nil
}

func result(removed, kept, pinned int) string {
	switch {
	case removed+kept == 0:
		return fmt.Sprintf("none outdated (%d pinned)", pinned)
	case kept == 0:
		return fmt.Sprintf("removed %d", removed)
	}
	return fmt.Sprintf("removed %d, kept %d still in use", removed, kept)
}
