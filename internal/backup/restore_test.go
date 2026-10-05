package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/restic"
)

func TestRestore(t *testing.T) {
	type Given struct {
		running    bool
		runningErr error
		volumes    []string
		overwrite  bool
	}
	type Then struct {
		restored bool
		err      string
		aside    bool
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"an empty data directory": {Then: Then{restored: true}},
		"only configarr":          {Given: Given{volumes: []string{"configarr"}}, Then: Then{restored: true}},
		"the stack is running": {
			Given: Given{running: true},
			Then:  Then{err: "the stack is running; stop it with mse stack down first"},
		},
		"docker cannot tell": {
			Given: Given{runningErr: errors.New("cannot connect")},
			Then:  Then{err: "cannot tell whether the stack is running (cannot connect)"},
		},
		"app data without --overwrite": {
			Given: Given{volumes: []string{"sonarr"}},
			Then:  Then{err: "volumes/ already holds app data; run with --overwrite to replace it with the latest backup"},
		},
		"app data with --overwrite": {
			Given: Given{volumes: []string{"sonarr"}, overwrite: true},
			Then:  Then{restored: true, aside: true},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			b, m, out, _ := fixture(t)
			volumes := filepath.Join(b.Installation.Data, "volumes")
			require.NoError(t, os.RemoveAll(volumes))
			for _, app := range tt.Given.volumes {
				require.NoError(t, os.MkdirAll(filepath.Join(volumes, app), 0o755))
			}
			m.stack.EXPECT().AnyRunning(mock.Anything).Return(tt.Given.running, tt.Given.runningErr)
			if tt.Then.restored {
				m.repository.EXPECT().Unlock(mock.Anything).Return(nil)
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return([]restic.Snapshot{snapshot("this", "pi", "2026-10-04 04:30")}, nil)
				m.repository.EXPECT().Restore(mock.Anything, restic.RestoreOptions{Snapshot: "latest:/volumes", Host: "gorgon", Target: volumes, Exclude: []string{"configarr"}}).Return(nil)
			}

			err := b.Restore(context.Background(), tt.Given.overwrite)

			if tt.Then.err != "" {
				assert.EqualError(t, err, tt.Then.err)
				return
			}
			require.NoError(t, err)
			aside := filepath.Join(b.Installation.Data, "volumes.before-restore-20261005-043000")
			if tt.Then.aside {
				assert.DirExists(t, filepath.Join(aside, "sonarr"))
				assert.Equal(t, "previous volumes/ kept in "+aside+"; delete it once the restore looks right\nRestoring volumes/ from the latest backup... restored.\n", out.String())
				entries, readErr := os.ReadDir(volumes)
				require.NoError(t, readErr)
				assert.Empty(t, entries)
			} else {
				assert.NoDirExists(t, aside)
				assert.Equal(t, "Restoring volumes/ from the latest backup... restored.\n", out.String())
			}
		})
	}
}

func TestRestoreStopsWhenItCannotReadTheSnapshots(t *testing.T) {
	b, m, _, _ := fixture(t)
	require.NoError(t, os.RemoveAll(filepath.Join(b.Installation.Data, "volumes")))
	m.stack.EXPECT().AnyRunning(mock.Anything).Return(false, nil)
	m.repository.EXPECT().Unlock(mock.Anything).Return(nil)
	m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(nil, errors.New("restic snapshots failed (exit 1)"))

	err := b.Restore(context.Background(), false)

	assert.EqualError(t, err, "cannot read the backup repository's snapshots (restic snapshots failed (exit 1)); nothing was restored")
}
