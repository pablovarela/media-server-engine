package cmd

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/runs"
)

func TestRemoveExecutableDownloadsWithoutKeys(t *testing.T) {
	getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
	root := NewRootCommand(Dependencies{Environment: getenv, Home: home, Update: newMockUpdater(t)})
	var stdout bytes.Buffer
	root.SetOut(&stdout)

	code := run(context.Background(), root, []string{"clean-downloads"})

	assert.Equal(t, 0, code)
	assert.Equal(t, "sonarr: queue not reachable, skipped\nradarr: queue not reachable, skipped\n", stdout.String())
	run, recorded, err := runs.Read(filepath.Join(home, ".local", "state", "mse", "gorgon", "runs"), "download-cleanup")
	require.NoError(t, err)
	assert.True(t, recorded, "the run is recorded for mse status")
	assert.False(t, run.Ended.IsZero())
	assert.False(t, run.Failed)
}
