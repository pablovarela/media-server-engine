package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
		"only the wiring's state": {Given: Given{volumes: []string{".wiring", "configarr"}}, Then: Then{restored: true}},
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

func TestHasAppData(t *testing.T) {
	b, _, _, _ := fixture(t)
	held, err := b.HasAppData()
	require.NoError(t, err)
	assert.True(t, held, "the fixture has volumes/jellyfin")

	require.NoError(t, os.RemoveAll(filepath.Join(b.Installation.Data, "volumes", "jellyfin")))
	require.NoError(t, os.MkdirAll(filepath.Join(b.Installation.Data, "volumes", "configarr"), 0o755))
	held, err = b.HasAppData()
	require.NoError(t, err)
	assert.False(t, held, "configarr alone is not app data")

	require.NoError(t, os.MkdirAll(filepath.Join(b.Installation.Data, "volumes", ".wiring"), 0o755))
	held, err = b.HasAppData()
	require.NoError(t, err)
	assert.False(t, held, "the wiring's own state is not app data")

	b.Installation.Data = filepath.Join(t.TempDir(), "missing")
	held, err = b.HasAppData()
	require.NoError(t, err)
	assert.False(t, held)
}

func TestRestoreMedia(t *testing.T) {
	theirs := []restic.Snapshot{snapshot("this", "pi", "2026-10-04 01:40")}
	tests := map[string]struct {
		expect func(b *Backups, m mocks)
		out    string
		err    string
	}{
		"restores into data/media": {
			expect: func(b *Backups, m mocks) {
				target, _ := filepath.EvalSymlinks(MediaDir(b.Installation.Data))
				m.stack.EXPECT().AnyRunning(mock.Anything).Return(false, nil)
				m.media.EXPECT().Snapshots(mock.Anything, "gorgon").Return(theirs, nil)
				m.media.EXPECT().Unlock(mock.Anything).Return(nil)
				m.media.EXPECT().Restore(mock.Anything, restic.RestoreOptions{Snapshot: "latest", Host: "gorgon", Target: target, Overwrite: "if-changed"}).Return(nil)
			},
			out: "Restoring the media from the latest media backup into <target> (1834 GB free; files already there that match are kept)... restored.\n",
		},
		"the stack is running": {
			expect: func(_ *Backups, m mocks) { m.stack.EXPECT().AnyRunning(mock.Anything).Return(true, nil) },
			err:    "the stack is running; stop it with mse stack down first",
		},
		"no media backup yet": {
			expect: func(_ *Backups, m mocks) {
				m.stack.EXPECT().AnyRunning(mock.Anything).Return(false, nil)
				m.media.EXPECT().Snapshots(mock.Anything, "gorgon").Return(nil, nil)
			},
			err: "gorgon has no media backup yet; start without it with mse apply",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			b, m, out, _ := fixture(t)
			tt.expect(b, m)

			err := b.RestoreMedia(context.Background())

			if tt.err != "" {
				assert.EqualError(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			target, _ := filepath.EvalSymlinks(MediaDir(b.Installation.Data))
			assert.Equal(t, strings.ReplaceAll(tt.out, "<target>", target), out.String())
		})
	}
}

func TestRestoreMediaIntoAFolderOnAnotherDisk(t *testing.T) {
	b, m, _, _ := fixture(t)
	elsewhere := t.TempDir()
	require.NoError(t, os.RemoveAll(MediaDir(b.Installation.Data)))
	require.NoError(t, os.Symlink(elsewhere, MediaDir(b.Installation.Data)))
	resolved, _ := filepath.EvalSymlinks(elsewhere)
	m.stack.EXPECT().AnyRunning(mock.Anything).Return(false, nil)
	m.media.EXPECT().Snapshots(mock.Anything, "gorgon").Return([]restic.Snapshot{snapshot("this", "pi", "2026-10-04 01:40")}, nil)
	m.media.EXPECT().Unlock(mock.Anything).Return(nil)
	m.media.EXPECT().Restore(mock.Anything, mock.MatchedBy(func(o restic.RestoreOptions) bool { return o.Target == resolved })).Return(nil)

	require.NoError(t, b.RestoreMedia(context.Background()))
}

func TestRestoreMediaCreatesAMissingMediaFolder(t *testing.T) {
	b, m, _, _ := fixture(t)
	require.NoError(t, os.RemoveAll(MediaDir(b.Installation.Data)))
	m.stack.EXPECT().AnyRunning(mock.Anything).Return(false, nil)
	m.media.EXPECT().Snapshots(mock.Anything, "gorgon").Return([]restic.Snapshot{snapshot("this", "pi", "2026-10-04 01:40")}, nil)
	m.media.EXPECT().Unlock(mock.Anything).Return(nil)
	m.media.EXPECT().Restore(mock.Anything, mock.Anything).Return(nil)

	require.NoError(t, b.RestoreMedia(context.Background()))
	assert.DirExists(t, MediaDir(b.Installation.Data))
}

func TestRestoreMediaClearsThePendingRestore(t *testing.T) {
	b, m, _, _ := fixture(t)
	require.NoError(t, MarkMediaRestorePending(b.Installation.Data))
	m.stack.EXPECT().AnyRunning(mock.Anything).Return(false, nil)
	m.media.EXPECT().Snapshots(mock.Anything, "gorgon").Return([]restic.Snapshot{snapshot("this", "pi", "2026-10-04 01:40")}, nil)
	m.media.EXPECT().Unlock(mock.Anything).Return(nil)
	m.media.EXPECT().Restore(mock.Anything, mock.Anything).Return(nil)

	require.NoError(t, b.RestoreMedia(context.Background()))
	assert.False(t, MediaRestorePending(b.Installation.Data))
}

func TestAFailedMediaRestoreStaysPending(t *testing.T) {
	b, m, _, _ := fixture(t)
	require.NoError(t, MarkMediaRestorePending(b.Installation.Data))
	m.stack.EXPECT().AnyRunning(mock.Anything).Return(false, nil)
	m.media.EXPECT().Snapshots(mock.Anything, "gorgon").Return([]restic.Snapshot{snapshot("this", "pi", "2026-10-04 01:40")}, nil)
	m.media.EXPECT().Unlock(mock.Anything).Return(nil)
	m.media.EXPECT().Restore(mock.Anything, mock.Anything).Return(errors.New("restic restore failed (exit 1)"))

	require.Error(t, b.RestoreMedia(context.Background()))
	assert.True(t, MediaRestorePending(b.Installation.Data))
}

func TestRestoreMediaWaitsForNoMediaBackup(t *testing.T) {
	b, m, _, _ := fixture(t)
	held, err := takeLock(filepath.Join(b.Installation.Data, ".media-backup.lock"), "media backup")
	require.NoError(t, err)
	defer held.release()
	m.stack.EXPECT().AnyRunning(mock.Anything).Return(false, nil)

	err = b.RestoreMedia(context.Background())

	assert.ErrorContains(t, err, "a media backup is already running")
}
