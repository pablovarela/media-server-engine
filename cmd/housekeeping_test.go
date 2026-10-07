package cmd

import (
	"bytes"
	"context"
	"os"
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

func TestCleanDownloadsStillCleansWhenItsRunCannotBeRecorded(t *testing.T) {
	getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
	state := filepath.Join(home, ".local", "state", "mse", "gorgon")
	require.NoError(t, os.MkdirAll(state, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(state, "runs"), nil, 0o644))
	root := NewRootCommand(Dependencies{Environment: getenv, Home: home, Update: newMockUpdater(t)})
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"clean-downloads"})

	assert.Equal(t, 0, code)
	assert.Contains(t, stdout.String(), "sonarr: queue not reachable, skipped\n")
	assert.Contains(t, stderr.String(), "could not record the download-cleanup run for mse status")
}
