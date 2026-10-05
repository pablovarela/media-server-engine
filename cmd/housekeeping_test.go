package cmd

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pablovarela/media-server-engine/internal/images"
)

func TestPruneStackImagesWithoutDocker(t *testing.T) {
	getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
	deps := Dependencies{
		Environment: getenv, Home: home, Update: newMockUpdater(t),
		Images: func() (images.Client, error) { return nil, errors.New("no docker") },
	}
	root := NewRootCommand(deps)
	var stderr bytes.Buffer
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"prune-stack-images"})

	assert.Equal(t, 1, code)
	assert.Equal(t, "mse: no docker\n", stderr.String())
}

func TestRemoveExecutableDownloadsWithoutKeys(t *testing.T) {
	getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
	root := NewRootCommand(Dependencies{Environment: getenv, Home: home, Update: newMockUpdater(t)})
	var stdout bytes.Buffer
	root.SetOut(&stdout)

	code := run(context.Background(), root, []string{"remove-executable-downloads"})

	assert.Equal(t, 0, code)
	assert.Equal(t, "sonarr: queue not reachable, skipped\nradarr: queue not reachable, skipped\n", stdout.String())
}
