package cmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRemoveExecutableDownloadsWithoutKeys(t *testing.T) {
	getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
	root := NewRootCommand(Dependencies{Environment: getenv, Home: home, Update: newMockUpdater(t)})
	var stdout bytes.Buffer
	root.SetOut(&stdout)

	code := run(context.Background(), root, []string{"remove-executable-downloads"})

	assert.Equal(t, 0, code)
	assert.Equal(t, "sonarr: queue not reachable, skipped\nradarr: queue not reachable, skipped\n", stdout.String())
}
