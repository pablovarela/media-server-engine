package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/restic"
)

func notCancelled(ctx context.Context) bool { return ctx.Err() == nil }

func TestBackup(t *testing.T) {
	ours := []restic.Snapshot{snapshot("this", "pi", "2026-10-04 04:30")}
	backupOptions := func(b *Backups) any {
		resolved, _ := filepath.EvalSymlinks(b.Installation.Data)
		return mock.MatchedBy(func(o restic.BackupOptions) bool {
			return o.Host == "gorgon" && assert.ObjectsAreEqual([]string{"machine:this", "machine-name:pi", "nightly"}, o.Tags) &&
				o.ExcludeFile == "/state/backup-excludes.txt" && o.Dir == resolved &&
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
				m.stack.EXPECT().Stop(mock.Anything).Return("stopped", nil)
				m.repository.EXPECT().Backup(mock.Anything, backupOptions(b)).Return(restic.BackupSummary{SnapshotID: "40c4a929f0d1e2b3"}, nil)
				m.stack.EXPECT().Start(mock.Anything, []string{"jellyfin", "sonarr"}).Return("started", nil)
				m.repository.EXPECT().Forget(mock.Anything, "gorgon", mock.Anything).Return(restic.ForgetSummary{Kept: 5}, nil)
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "").Return()
			}},
			Then: Then{out: "Stopping the stack... stopped.\nBacking up volumes/... snapshot 40c4a929: 0 new, 0 changed, 0 unchanged files; 0 B added (0 B stored).\nStarting the services again... started.\nRemoving old snapshots... kept 5, removed 0.\nBackup done.\n", marked: true},
		},
		"nothing was running": {
			Given: Given{expect: func(b *Backups, m mocks, _ context.CancelFunc) {
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(nil, nil)
				m.stack.EXPECT().RunningServices(mock.Anything).Return(nil, nil)
				m.repository.EXPECT().Unlock(mock.Anything).Return(nil)
				m.stack.EXPECT().Stop(mock.Anything).Return("stopped", nil)
				m.repository.EXPECT().Backup(mock.Anything, backupOptions(b)).Return(restic.BackupSummary{SnapshotID: "40c4a929f0d1e2b3"}, nil)
				m.repository.EXPECT().Forget(mock.Anything, "gorgon", mock.Anything).Return(restic.ForgetSummary{Kept: 5}, nil)
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "").Return()
			}},
			Then: Then{out: "Stopping the stack... stopped.\nBacking up volumes/... snapshot 40c4a929: 0 new, 0 changed, 0 unchanged files; 0 B added (0 B stored).\nRemoving old snapshots... kept 5, removed 0.\nBackup done.\n", marked: true},
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
				m.stack.EXPECT().Stop(mock.Anything).Return("stopped", nil)
				m.repository.EXPECT().Backup(mock.Anything, backupOptions(b)).Return(restic.BackupSummary{}, errors.New("restic backup failed (exit 3)"))
				m.stack.EXPECT().Start(mock.Anything, []string{"jellyfin"}).Return("started", nil)
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/fail").Return()
			}},
			Then: Then{out: "Stopping the stack... stopped.\nBacking up volumes/... failed.\nStarting the services again... started.\n", err: "restic backup failed (exit 3)"},
		},
		"interrupted mid-backup": {
			Given: Given{expect: func(b *Backups, m mocks, cancel context.CancelFunc) {
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(ours, nil)
				m.stack.EXPECT().RunningServices(mock.Anything).Return([]string{"jellyfin"}, nil)
				m.repository.EXPECT().Unlock(mock.Anything).Return(nil)
				m.stack.EXPECT().Stop(mock.Anything).Return("stopped", nil)
				m.repository.EXPECT().Backup(mock.Anything, backupOptions(b)).RunAndReturn(func(context.Context, restic.BackupOptions) (restic.BackupSummary, error) {
					cancel()
					return restic.BackupSummary{}, errors.New("restic was interrupted")
				})
				m.stack.EXPECT().Start(mock.MatchedBy(notCancelled), []string{"jellyfin"}).Return("started", nil)
				m.pinger.EXPECT().Ping(mock.MatchedBy(notCancelled), "backup", "/fail").Return()
			}},
			Then: Then{out: "Stopping the stack... stopped.\nBacking up volumes/... failed.\nStarting the services again... started.\n", err: "restic was interrupted"},
		},
		"listing the running services fails": {
			Given: Given{expect: func(b *Backups, m mocks, _ context.CancelFunc) {
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(ours, nil)
				m.stack.EXPECT().RunningServices(mock.Anything).Return(nil, errors.New("cannot connect"))
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/fail").Return()
			}},
			Then: Then{out: "Stopping the stack... failed.\n", err: "cannot connect"},
		},
		"unlock fails: nothing is stopped": {
			Given: Given{expect: func(b *Backups, m mocks, _ context.CancelFunc) {
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(ours, nil)
				m.stack.EXPECT().RunningServices(mock.Anything).Return([]string{"jellyfin"}, nil)
				m.repository.EXPECT().Unlock(mock.Anything).Return(errors.New("restic unlock failed (exit 1)"))
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/fail").Return()
			}},
			Then: Then{out: "Stopping the stack... failed.\n", err: "restic unlock failed (exit 1)"},
		},
		"starting after the backup fails: tried once more": {
			Given: Given{expect: func(b *Backups, m mocks, _ context.CancelFunc) {
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(ours, nil)
				m.stack.EXPECT().RunningServices(mock.Anything).Return([]string{"jellyfin"}, nil)
				m.repository.EXPECT().Unlock(mock.Anything).Return(nil)
				m.stack.EXPECT().Stop(mock.Anything).Return("stopped", nil)
				m.repository.EXPECT().Backup(mock.Anything, backupOptions(b)).Return(restic.BackupSummary{SnapshotID: "40c4a929f0d1e2b3"}, nil)
				m.stack.EXPECT().Start(mock.Anything, []string{"jellyfin"}).Return("", errors.New("port busy")).Once()
				m.stack.EXPECT().Start(mock.Anything, []string{"jellyfin"}).Return("started", nil).Once()
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/fail").Return()
			}},
			Then: Then{out: "Stopping the stack... stopped.\nBacking up volumes/... snapshot 40c4a929: 0 new, 0 changed, 0 unchanged files; 0 B added (0 B stored).\nStarting the services again... failed.\nStarting the services again... started.\n", err: "port busy"},
		},
		"forget fails: the services stay up": {
			Given: Given{expect: func(b *Backups, m mocks, _ context.CancelFunc) {
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(ours, nil)
				m.stack.EXPECT().RunningServices(mock.Anything).Return([]string{"jellyfin"}, nil)
				m.repository.EXPECT().Unlock(mock.Anything).Return(nil)
				m.stack.EXPECT().Stop(mock.Anything).Return("stopped", nil)
				m.repository.EXPECT().Backup(mock.Anything, backupOptions(b)).Return(restic.BackupSummary{SnapshotID: "40c4a929f0d1e2b3"}, nil)
				m.stack.EXPECT().Start(mock.Anything, []string{"jellyfin"}).Return("started", nil).Once()
				m.repository.EXPECT().Forget(mock.Anything, "gorgon", mock.Anything).Return(restic.ForgetSummary{}, errors.New("restic forget failed (exit 1)"))
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/fail").Return()
			}},
			Then: Then{out: "Stopping the stack... stopped.\nBacking up volumes/... snapshot 40c4a929: 0 new, 0 changed, 0 unchanged files; 0 B added (0 B stored).\nStarting the services again... started.\nRemoving old snapshots... failed.\n", err: "restic forget failed (exit 1)"},
		},
		"starting again fails too": {
			Given: Given{expect: func(b *Backups, m mocks, _ context.CancelFunc) {
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
				m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(ours, nil)
				m.stack.EXPECT().RunningServices(mock.Anything).Return([]string{"jellyfin"}, nil)
				m.repository.EXPECT().Unlock(mock.Anything).Return(nil)
				m.stack.EXPECT().Stop(mock.Anything).Return("", errors.New("docker is gone"))
				m.stack.EXPECT().Start(mock.Anything, []string{"jellyfin"}).Return("", errors.New("docker is still gone"))
				m.pinger.EXPECT().Ping(mock.Anything, "backup", "/fail").Return()
			}},
			Then: Then{out: "Stopping the stack... failed.\nStarting the services again... failed.\n", err: "docker is gone\ndocker is still gone"},
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
		m.stack.EXPECT().Stop(mock.Anything).Return("stopped", nil)
		m.repository.EXPECT().Backup(mock.Anything, mock.Anything).Return(restic.BackupSummary{SnapshotID: "40c4a929f0d1e2b3"}, nil)
		m.repository.EXPECT().Forget(mock.Anything, "gorgon", mock.Anything).Return(restic.ForgetSummary{Kept: 5}, nil)
		m.pinger.EXPECT().Ping(mock.Anything, "backup", "").Return()
	}
	claimed := "Stopping the stack... stopped.\nBacking up volumes/... snapshot 40c4a929: 0 new, 0 changed, 0 unchanged files; 0 B added (0 B stored).\nRemoving old snapshots... kept 5, removed 0.\nBackup done.\nThis machine is now gorgon's main; backups from any other machine are refused.\n"
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
			Then: Then{out: "Creating the backup repository b2:bucket... created.\n" + claimed},
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

func TestTheRestartIsShieldedFromSignals(t *testing.T) {
	b, m, _, _ := fixture(t)
	shielded := false
	b.Shield = func() func() {
		shielded = true
		return func() { shielded = false }
	}
	m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
	m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(nil, nil)
	m.stack.EXPECT().RunningServices(mock.Anything).Return([]string{"jellyfin"}, nil)
	m.repository.EXPECT().Unlock(mock.Anything).Return(nil)
	m.stack.EXPECT().Stop(mock.Anything).RunAndReturn(func(context.Context) (string, error) {
		assert.True(t, shielded, "signals are shielded from the moment the stack stops")
		return "stopped", nil
	})
	m.repository.EXPECT().Backup(mock.Anything, mock.Anything).RunAndReturn(func(context.Context, restic.BackupOptions) (restic.BackupSummary, error) {
		assert.True(t, shielded, "signals are shielded while restic runs with the stack stopped")
		return restic.BackupSummary{}, errors.New("restic was interrupted")
	})
	m.stack.EXPECT().Start(mock.Anything, []string{"jellyfin"}).RunAndReturn(func(context.Context, []string) (string, error) {
		assert.True(t, shielded, "signals are shielded while the services start again")
		return "started", nil
	})
	m.pinger.EXPECT().Ping(mock.Anything, "backup", "/fail").Return()

	assert.EqualError(t, b.Backup(context.Background()), "restic was interrupted")
	assert.False(t, shielded, "the shield is released")
}

func TestBackupRunsResticInTheResolvedDataDirectory(t *testing.T) {
	b, m, _, _ := fixture(t)
	real := b.Installation.Data
	require.NoError(t, os.MkdirAll(filepath.Join(real, "volumes", "sonarr"), 0o755))
	linked := filepath.Join(t.TempDir(), "linked")
	require.NoError(t, os.Symlink(real, linked))
	b.Installation.Data = linked
	resolved, err := filepath.EvalSymlinks(real)
	require.NoError(t, err)
	m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
	m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(nil, nil)
	m.stack.EXPECT().RunningServices(mock.Anything).Return(nil, nil)
	m.repository.EXPECT().Unlock(mock.Anything).Return(nil)
	m.stack.EXPECT().Stop(mock.Anything).Return("stopped", nil)
	m.repository.EXPECT().Backup(mock.Anything, mock.MatchedBy(func(o restic.BackupOptions) bool { return o.Dir == resolved })).Return(restic.BackupSummary{SnapshotID: "40c4a929f0d1e2b3"}, nil)
	m.repository.EXPECT().Forget(mock.Anything, "gorgon", mock.Anything).Return(restic.ForgetSummary{Kept: 5}, nil)
	m.pinger.EXPECT().Ping(mock.Anything, "backup", "").Return()

	require.NoError(t, b.Backup(context.Background()))
}

func TestBackupRefusesAnEmptyDataDirectory(t *testing.T) {
	b, m, _, _ := fixture(t)
	require.NoError(t, os.RemoveAll(filepath.Join(b.Installation.Data, "volumes")))
	m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
	m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(nil, nil)
	m.pinger.EXPECT().Ping(mock.Anything, "backup", "/fail").Return()

	err := b.Backup(context.Background())

	assert.EqualError(t, err, "volumes/ in "+b.Installation.Data+" holds no app data; nothing was backed up")
}

func TestBackupFailsWhenItCannotMarkTheMain(t *testing.T) {
	b, m, _, _ := fixture(t)
	require.NoError(t, os.Mkdir(filepath.Join(b.Installation.Data, ".backup-main"), 0o755))
	m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
	m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(nil, nil)
	m.stack.EXPECT().RunningServices(mock.Anything).Return(nil, nil)
	m.repository.EXPECT().Unlock(mock.Anything).Return(nil)
	m.stack.EXPECT().Stop(mock.Anything).Return("stopped", nil)
	m.repository.EXPECT().Backup(mock.Anything, mock.Anything).Return(restic.BackupSummary{SnapshotID: "40c4a929f0d1e2b3"}, nil)
	m.repository.EXPECT().Forget(mock.Anything, "gorgon", mock.Anything).Return(restic.ForgetSummary{Kept: 5}, nil)
	m.pinger.EXPECT().Ping(mock.Anything, "backup", "/fail").Return()

	assert.ErrorContains(t, b.Backup(context.Background()), ".backup-main")
}

func TestOldSnapshotsArePrunedOnlyWhenSomeWereRemoved(t *testing.T) {
	b, m, out, _ := fixture(t)
	removed := time.Date(2026, 10, 5, 4, 31, 0, 0, time.UTC)
	m.pinger.EXPECT().Ping(mock.Anything, "backup", "/start").Return()
	m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(nil, nil)
	m.stack.EXPECT().RunningServices(mock.Anything).Return(nil, nil)
	m.repository.EXPECT().Unlock(mock.Anything).Return(nil)
	m.stack.EXPECT().Stop(mock.Anything).Return("stopped", nil)
	m.repository.EXPECT().Backup(mock.Anything, mock.Anything).Return(restic.BackupSummary{SnapshotID: "40c4a929f0d1e2b3"}, nil)
	m.repository.EXPECT().Forget(mock.Anything, "gorgon", mock.Anything).Return(restic.ForgetSummary{Kept: 5, Removed: []time.Time{removed}}, nil)
	m.repository.EXPECT().Prune(mock.Anything, mock.Anything).Return(nil)
	m.pinger.EXPECT().Ping(mock.Anything, "backup", "").Return()

	require.NoError(t, b.Backup(context.Background()))

	assert.Contains(t, out.String(), "Removing old snapshots... kept 5, removed 1 (2026-10-05 04:31); pruned.\n")
}
