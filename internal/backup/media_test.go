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

var ours = []restic.Snapshot{snapshot("this", "pi", "2026-10-04 04:30")}

func mediaOptions(b *Backups, limit int) any {
	resolved, _ := filepath.EvalSymlinks(MediaDir(b.Installation.Data))
	return mock.MatchedBy(func(o restic.BackupOptions) bool {
		return o.Host == "gorgon" && assert.ObjectsAreEqual([]string{"machine:this", "machine-name:pi", "weekly"}, o.Tags) &&
			o.ExcludeFile == "" && o.Dir == resolved && assert.ObjectsAreEqual([]string{"."}, o.Paths) &&
			o.LimitUpload == limit && len(o.Inherit) == 1 && o.Progress != nil
	})
}

func backsUpMedia(b *Backups, m mocks, exists bool, limit int) {
	m.pinger.EXPECT().Ping(mock.Anything, "media-backup", "/start").Return()
	m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(ours, nil)
	m.media.EXPECT().HasRepository(mock.Anything).Return(exists, nil)
	if !exists {
		m.media.EXPECT().Init(mock.Anything).Return(nil)
	}
	m.media.EXPECT().Unlock(mock.Anything).Return(nil)
	m.media.EXPECT().Backup(mock.Anything, mediaOptions(b, limit)).Return(restic.BackupSummary{SnapshotID: "7d2e9c41aa00"}, nil)
	m.media.EXPECT().Forget(mock.Anything, "gorgon", restic.Keep{Weekly: 4, GroupBy: "host"}, mock.Anything).Return(restic.ForgetSummary{Kept: 4}, nil)
}

func checkedMedia(m mocks) {
	m.media.EXPECT().Check(mock.Anything, "5%").Return(nil)
	m.pinger.EXPECT().Ping(mock.Anything, "media-backup", "").Return()
}

func TestBackupMedia(t *testing.T) {
	type Given struct {
		limit  int
		expect func(b *Backups, m mocks)
	}
	type Then struct{ out, err string }
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"the main backs up its media": {
			Given: Given{expect: func(b *Backups, m mocks) { backsUpMedia(b, m, true, 0); checkedMedia(m) }},
			Then: Then{out: "Backing up the media... snapshot 7d2e9c41: 0 new, 0 changed, 0 unchanged files; 0 B added (0 B stored).\n" +
				"Removing old media snapshots... kept 4, removed 0.\nChecking 5% of the media backup... done.\nMedia backup done.\n"},
		},
		"with an upload cap": {
			Given: Given{limit: 2048, expect: func(b *Backups, m mocks) { backsUpMedia(b, m, true, 2048); checkedMedia(m) }},
		},
		"the first run creates the repository": {
			Given: Given{expect: func(b *Backups, m mocks) { backsUpMedia(b, m, false, 0); checkedMedia(m) }},
			Then: Then{out: "Creating the media backup repository b2:bucket:restic-media... created.\n" +
				"Backing up the media... snapshot 7d2e9c41: 0 new, 0 changed, 0 unchanged files; 0 B added (0 B stored).\n" +
				"Removing old media snapshots... kept 4, removed 0.\nChecking 5% of the media backup... done.\nMedia backup done.\n"},
		},
		"another machine is the main: refused, not failed": {
			Given: Given{expect: func(b *Backups, m mocks) {
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return([]restic.Snapshot{snapshot("other", "pi2", "2026-10-04 04:30")}, nil)
			}},
			Then: Then{out: "gorgon's main is pi2, last backup 2026-10-04 04:30. Its media is backed up there; mse backup --apps --take-over makes this machine the main.\n"},
		},
		"the apps repository can't be read": {
			Given: Given{expect: func(b *Backups, m mocks) {
				m.pinger.EXPECT().Ping(mock.Anything, "media-backup", "/start").Return()
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(nil, errors.New("restic snapshots failed (exit 1)"))
				m.pinger.EXPECT().Ping(mock.Anything, "media-backup", "/fail").Return()
			}},
			Then: Then{err: "cannot read the backup repository to tell which machine is gorgon's main (restic snapshots failed (exit 1)); nothing was backed up"},
		},
		"the key can't reach the repository": {
			Given: Given{expect: func(b *Backups, m mocks) {
				m.pinger.EXPECT().Ping(mock.Anything, "media-backup", "/start").Return()
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(ours, nil)
				m.media.EXPECT().HasRepository(mock.Anything).Return(false, errors.New("restic cat failed (exit 1): Fatal: unable to open config file: b2_download_file_by_name: 401: unauthorized"))
				m.pinger.EXPECT().Ping(mock.Anything, "media-backup", "/fail").Return()
			}},
			Then: Then{err: "restic cat failed (exit 1): Fatal: unable to open config file: b2_download_file_by_name: 401: unauthorized\n" +
				"The storage refused the key for b2:bucket:restic-media. A B2 key limited to a file-name prefix can't reach a repository outside it; give the media backup a key that covers the whole bucket"},
		},
		"the check fails": {
			Given: Given{expect: func(b *Backups, m mocks) {
				backsUpMedia(b, m, true, 0)
				m.media.EXPECT().Check(mock.Anything, "5%").Return(errors.New("restic check failed (exit 1)"))
				m.pinger.EXPECT().Ping(mock.Anything, "media-backup", "/fail").Return()
			}},
			Then: Then{err: "restic check failed (exit 1)"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			b, m, out, _ := fixture(t)
			b.Media.UploadLimit = tt.Given.limit
			tt.Given.expect(b, m)

			err := b.BackupMedia(context.Background())

			if tt.Then.err != "" {
				assert.EqualError(t, err, tt.Then.err)
				return
			}
			require.NoError(t, err)
			if tt.Then.out != "" {
				assert.Equal(t, tt.Then.out, out.String())
			}
		})
	}
}

func TestBackupMediaOffTheMainRemovesTheMarker(t *testing.T) {
	b, m, _, _ := fixture(t)
	marker := filepath.Join(b.Installation.Data, ".backup-main")
	require.NoError(t, os.WriteFile(marker, nil, 0o644))
	m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return([]restic.Snapshot{snapshot("other", "pi2", "2026-10-04 04:30")}, nil)

	require.NoError(t, b.BackupMedia(context.Background()))
	assert.NoFileExists(t, marker)
}

func TestBackupMediaReadsAMediaFolderOnAnotherDisk(t *testing.T) {
	b, m, _, _ := fixture(t)
	elsewhere := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(elsewhere, "film.mkv"), []byte("film"), 0o644))
	require.NoError(t, os.RemoveAll(MediaDir(b.Installation.Data)))
	require.NoError(t, os.Symlink(elsewhere, MediaDir(b.Installation.Data)))
	resolved, _ := filepath.EvalSymlinks(elsewhere)
	m.pinger.EXPECT().Ping(mock.Anything, "media-backup", mock.Anything).Return()
	m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(nil, nil)
	m.media.EXPECT().HasRepository(mock.Anything).Return(true, nil)
	m.media.EXPECT().Unlock(mock.Anything).Return(nil)
	m.media.EXPECT().Backup(mock.Anything, mock.MatchedBy(func(o restic.BackupOptions) bool { return o.Dir == resolved })).Return(restic.BackupSummary{}, nil)
	m.media.EXPECT().Forget(mock.Anything, "gorgon", mock.Anything, mock.Anything).Return(restic.ForgetSummary{}, nil)
	m.media.EXPECT().Check(mock.Anything, mock.Anything).Return(nil)

	require.NoError(t, b.BackupMedia(context.Background()))
}

func TestBackupMediaWithoutAMediaFolder(t *testing.T) {
	b, m, _, _ := fixture(t)
	require.NoError(t, os.RemoveAll(MediaDir(b.Installation.Data)))
	m.pinger.EXPECT().Ping(mock.Anything, "media-backup", "/start").Return()
	m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(nil, nil)
	m.pinger.EXPECT().Ping(mock.Anything, "media-backup", "/fail").Return()

	err := b.BackupMedia(context.Background())

	assert.ErrorContains(t, err, filepath.Join("data", "media"))
	assert.ErrorContains(t, err, "nothing was backed up")
}

func TestOneMediaBackupAtATime(t *testing.T) {
	b, _, _, _ := fixture(t)
	held, err := takeLock(filepath.Join(b.Installation.Data, ".media-backup.lock"), "media backup")
	require.NoError(t, err)
	defer held.release()

	err = b.BackupMedia(context.Background())

	assert.ErrorContains(t, err, "a media backup is already running")
}

func TestBackupMediaRefusesAnEmptyMediaFolder(t *testing.T) {
	b, m, _, _ := fixture(t)
	require.NoError(t, os.Remove(filepath.Join(MediaDir(b.Installation.Data), "film.mkv")))
	m.pinger.EXPECT().Ping(mock.Anything, "media-backup", "/start").Return()
	m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(ours, nil)
	m.pinger.EXPECT().Ping(mock.Anything, "media-backup", "/fail").Return()

	err := b.BackupMedia(context.Background())

	assert.EqualError(t, err, MediaDir(b.Installation.Data)+" is empty (is its disk mounted?); nothing was backed up")
}

func TestBackupMediaRefusesWhileTheMediaIsStillToBeRestored(t *testing.T) {
	b, m, _, _ := fixture(t)
	require.NoError(t, MarkMediaRestorePending(b.Installation.Data))
	m.pinger.EXPECT().Ping(mock.Anything, "media-backup", "/start").Return()
	m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(ours, nil)
	m.pinger.EXPECT().Ping(mock.Anything, "media-backup", "/fail").Return()

	err := b.BackupMedia(context.Background())

	assert.EqualError(t, err, "gorgon's media hasn't been restored on this machine yet, so a backup now would replace the media backup with what is here; "+
		"mse restore --media brings it back")
}
