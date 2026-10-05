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

func notCancelled(ctx context.Context) bool { return ctx.Err() == nil }

func TestBackup(t *testing.T) {
	ours := []restic.Snapshot{snapshot("this", "pi", "2026-10-04 04:30")}
	backupOptions := func(b *Backups) any {
		return mock.MatchedBy(func(o restic.BackupOptions) bool {
			return o.Host == "gorgon" && assert.ObjectsAreEqual([]string{"machine:this", "machine-name:pi", "nightly"}, o.Tags) &&
				o.ExcludeFile == "/state/backup-excludes.txt" && o.Dir == b.Installation.Data &&
				assert.ObjectsAreEqual([]string{"volumes"}, o.Paths) && len(o.Inherit) == 1
		})
	}
	type Given struct {
		expect func(b *Backups, m mocks, cancel context.CancelFunc)
		marker bool
	}
	type Then struct {
		out    string
		err    string
		marked bool
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"the main backs up": {
			Given: Given{expect: func(b *Backups, m mocks, _ context.CancelFunc) {
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(ours, nil)
				m.stack.EXPECT().RunningServices(mock.Anything).Return([]string{"jellyfin", "sonarr"}, nil)
				m.repository.EXPECT().Unlock(mock.Anything).Return(nil)
				m.stack.EXPECT().Stop(mock.Anything).Return(nil)
				m.repository.EXPECT().Backup(mock.Anything, backupOptions(b)).Return(nil)
				m.stack.EXPECT().Start(mock.Anything, []string{"jellyfin", "sonarr"}).Return(nil)
				m.repository.EXPECT().Forget(mock.Anything, "gorgon", mock.Anything).Return(nil)
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "").Return()
			}},
			Then: Then{out: "Stopping the stack...\nBacking up volumes/...\nStarting 2 services...\nRemoving old snapshots...\nBackup done.\n", marked: true},
		},
		"nothing was running": {
			Given: Given{expect: func(b *Backups, m mocks, _ context.CancelFunc) {
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(nil, nil)
				m.stack.EXPECT().RunningServices(mock.Anything).Return(nil, nil)
				m.repository.EXPECT().Unlock(mock.Anything).Return(nil)
				m.stack.EXPECT().Stop(mock.Anything).Return(nil)
				m.repository.EXPECT().Backup(mock.Anything, backupOptions(b)).Return(nil)
				m.repository.EXPECT().Forget(mock.Anything, "gorgon", mock.Anything).Return(nil)
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "").Return()
			}},
			Then: Then{out: "Stopping the stack...\nBacking up volumes/...\nRemoving old snapshots...\nBackup done.\n", marked: true},
		},
		"another machine is the main": {
			Given: Given{marker: true, expect: func(b *Backups, m mocks, _ context.CancelFunc) {
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return([]restic.Snapshot{snapshot("other", "pi2", "2026-10-04 04:30")}, nil)
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/fail").Return()
			}},
			Then: Then{err: "another machine is gorgon's main; this machine does not back up (mse claim-backup-main makes it the main)"},
		},
		"unreadable repository": {
			Given: Given{expect: func(b *Backups, m mocks, _ context.CancelFunc) {
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(nil, errors.New("restic snapshots failed (exit 1)"))
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/fail").Return()
			}},
			Then: Then{err: "cannot read the backup repository to tell which machine is gorgon's main (restic snapshots failed (exit 1)); nothing was backed up"},
		},
		"restic fails: the services start again": {
			Given: Given{expect: func(b *Backups, m mocks, _ context.CancelFunc) {
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(ours, nil)
				m.stack.EXPECT().RunningServices(mock.Anything).Return([]string{"jellyfin"}, nil)
				m.repository.EXPECT().Unlock(mock.Anything).Return(nil)
				m.stack.EXPECT().Stop(mock.Anything).Return(nil)
				m.repository.EXPECT().Backup(mock.Anything, backupOptions(b)).Return(errors.New("restic backup failed (exit 3)"))
				m.stack.EXPECT().Start(mock.Anything, []string{"jellyfin"}).Return(nil)
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/fail").Return()
			}},
			Then: Then{out: "Stopping the stack...\nBacking up volumes/...\n", err: "restic backup failed (exit 3)"},
		},
		"interrupted mid-backup": {
			Given: Given{expect: func(b *Backups, m mocks, cancel context.CancelFunc) {
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(ours, nil)
				m.stack.EXPECT().RunningServices(mock.Anything).Return([]string{"jellyfin"}, nil)
				m.repository.EXPECT().Unlock(mock.Anything).Return(nil)
				m.stack.EXPECT().Stop(mock.Anything).Return(nil)
				m.repository.EXPECT().Backup(mock.Anything, backupOptions(b)).RunAndReturn(func(context.Context, restic.BackupOptions) error {
					cancel()
					return errors.New("restic was interrupted")
				})
				m.stack.EXPECT().Start(mock.MatchedBy(notCancelled), []string{"jellyfin"}).Return(nil)
				m.pinger.EXPECT().Ping(mock.MatchedBy(notCancelled), "backup", "/fail").Return()
			}},
			Then: Then{out: "Stopping the stack...\nBacking up volumes/...\n", err: "restic was interrupted"},
		},
		"starting again fails too": {
			Given: Given{expect: func(b *Backups, m mocks, _ context.CancelFunc) {
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(ours, nil)
				m.stack.EXPECT().RunningServices(mock.Anything).Return([]string{"jellyfin"}, nil)
				m.repository.EXPECT().Unlock(mock.Anything).Return(nil)
				m.stack.EXPECT().Stop(mock.Anything).Return(errors.New("docker is gone"))
				m.stack.EXPECT().Start(mock.Anything, []string{"jellyfin"}).Return(errors.New("docker is still gone"))
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/fail").Return()
			}},
			Then: Then{out: "Stopping the stack...\n", err: "docker is gone\ndocker is still gone"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			b, m, out, _ := fixture(t)
			marker := filepath.Join(b.Installation.Data, ".backup-main")
			if tt.Given.marker {
				require.NoError(t, os.WriteFile(marker, nil, 0o644))
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			tt.Given.expect(b, m, cancel)

			err := b.Backup(ctx)

			if tt.Then.err != "" {
				assert.EqualError(t, err, tt.Then.err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.Then.out, out.String())
			if tt.Then.marked {
				assert.FileExists(t, marker)
			} else {
				assert.NoFileExists(t, marker)
			}
		})
	}
}

func TestBackupRefusesWhileAnotherRuns(t *testing.T) {
	b, _, _, _ := fixture(t)
	held, err := takeLock(filepath.Join(b.Installation.Data, ".backup.lock"))
	require.NoError(t, err)
	defer held.release()

	err = b.Backup(context.Background())

	assert.ErrorContains(t, err, "a backup is already running (process ")
}

func TestClaim(t *testing.T) {
	theirs := []restic.Snapshot{snapshot("other", "pi2", "2026-10-04 04:30")}
	backsUp := func(m mocks) {
		m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
		m.stack.EXPECT().RunningServices(mock.Anything).Return(nil, nil)
		m.repository.EXPECT().Unlock(mock.Anything).Return(nil)
		m.stack.EXPECT().Stop(mock.Anything).Return(nil)
		m.repository.EXPECT().Backup(mock.Anything, mock.Anything).Return(nil)
		m.repository.EXPECT().Forget(mock.Anything, "gorgon", mock.Anything).Return(nil)
		m.pinger.EXPECT().Ping(mock.Anything, "backup", "").Return()
	}
	claimed := "Stopping the stack...\nBacking up volumes/...\nRemoving old snapshots...\nBackup done.\nThis machine is now gorgon's main; backups from any other machine are refused.\n"
	type Given struct {
		yes         bool
		answer      string
		interactive bool
		expect      func(m mocks)
	}
	type Then struct {
		out    string
		errOut string
		err    string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"a new repository": {
			Given: Given{expect: func(m mocks) {
				m.repository.EXPECT().HasRepository(mock.Anything).Return(false, nil)
				m.repository.EXPECT().Init(mock.Anything).Return(nil)
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(nil, nil)
				backsUp(m)
			}},
			Then: Then{out: "Creating the backup repository b2:bucket\n" + claimed},
		},
		"taking over with --yes": {
			Given: Given{yes: true, expect: func(m mocks) {
				m.repository.EXPECT().HasRepository(mock.Anything).Return(true, nil)
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(theirs, nil)
				backsUp(m)
			}},
			Then: Then{out: claimed},
		},
		"taking over when asked": {
			Given: Given{answer: "y", interactive: true, expect: func(m mocks) {
				m.repository.EXPECT().HasRepository(mock.Anything).Return(true, nil)
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(theirs, nil)
				backsUp(m)
			}},
			Then: Then{out: claimed, errOut: "gorgon's main is pi2, last backup 2026-10-04 04:30. Taking over makes it refuse to back up.\n"},
		},
		"declined": {
			Given: Given{answer: "n", interactive: true, expect: func(m mocks) {
				m.repository.EXPECT().HasRepository(mock.Anything).Return(true, nil)
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(theirs, nil)
			}},
			Then: Then{errOut: "gorgon's main is pi2, last backup 2026-10-04 04:30. Taking over makes it refuse to back up.\n", err: "nothing was claimed"},
		},
		"no terminal": {
			Given: Given{expect: func(m mocks) {
				m.repository.EXPECT().HasRepository(mock.Anything).Return(true, nil)
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(theirs, nil)
			}},
			Then: Then{errOut: "gorgon's main is pi2, last backup 2026-10-04 04:30. Taking over makes it refuse to back up.\n", err: "nothing was claimed; --yes takes over without asking"},
		},
		"unreadable repository": {
			Given: Given{yes: true, expect: func(m mocks) {
				m.repository.EXPECT().HasRepository(mock.Anything).Return(true, nil)
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(nil, errors.New("boom"))
			}},
			Then: Then{err: "cannot read the backup repository to tell which machine is gorgon's main (boom); nothing was claimed"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			b, m, out, errOut := fixture(t)
			b.Ask = func(string) (string, bool) { return tt.Given.answer, tt.Given.interactive }
			tt.Given.expect(m)

			err := b.Claim(context.Background(), tt.Given.yes)

			if tt.Then.err != "" {
				assert.EqualError(t, err, tt.Then.err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.Then.out, out.String())
			assert.Equal(t, tt.Then.errOut, errOut.String())
		})
	}
}
