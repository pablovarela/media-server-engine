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
