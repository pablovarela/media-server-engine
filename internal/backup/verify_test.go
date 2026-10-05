package backup

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/pablovarela/media-server-engine/internal/restic"
)

func goodDatabase(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.ExecContext(context.Background(), "CREATE TABLE t (x BLOB); WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 300) INSERT INTO t SELECT randomblob(200) FROM n;")
	require.NoError(t, err)
	require.NoError(t, db.Close())
}

func corruptDatabase(t *testing.T, path string) {
	t.Helper()
	goodDatabase(t, path)
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	require.NoError(t, err)
	garbage := make([]byte, 4096)
	for n := range garbage {
		garbage[n] = 0xff
	}
	_, err = file.WriteAt(garbage, 4096*2)
	require.NoError(t, err)
	require.NoError(t, file.Close())
}

func TestVerify(t *testing.T) {
	ours := []restic.Snapshot{snapshot("this", "pi", "2026-10-04 04:30")}
	type Given struct {
		snapshots []restic.Snapshot
		restored  func(t *testing.T, dir string)
	}
	type Then struct {
		out string
		err string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"good databases": {
			Given: Given{snapshots: ours, restored: func(t *testing.T, dir string) {
				goodDatabase(t, filepath.Join(dir, "volumes", "sonarr", "sonarr.db"))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "volumes", "sonarr", "notes.sqlite"), []byte("not sqlite"), 0o644))
			}},
			Then: Then{out: "Checking the repository...\nRestoring the databases of the latest snapshot...\nChecking 2 databases...\nThe backups check out.\n"},
		},
		"no databases": {
			Given: Given{snapshots: ours, restored: func(*testing.T, string) {}},
			Then:  Then{out: "Checking the repository...\nRestoring the databases of the latest snapshot...\n", err: "the latest snapshot holds no databases"},
		},
		"corrupt database": {
			Given: Given{snapshots: ours, restored: func(t *testing.T, dir string) {
				corruptDatabase(t, filepath.Join(dir, "volumes", "radarr", "radarr.db"))
			}},
			Then: Then{out: "Checking the repository...\nRestoring the databases of the latest snapshot...\nChecking 1 databases...\n", err: "volumes/radarr/radarr.db"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			b, m, out, _ := fixture(t)
			m.pinger.EXPECT().Ping(mock.Anything, "verify", "/start").Return()
			m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(tt.Given.snapshots, nil)
			m.repository.EXPECT().Unlock(mock.Anything).Return(nil)
			m.repository.EXPECT().Check(mock.Anything).Return(nil)
			m.repository.EXPECT().Restore(mock.Anything, mock.MatchedBy(func(o restic.RestoreOptions) bool {
				return o.Snapshot == "latest" && o.Host == "gorgon" && filepath.Dir(o.Target) == b.TempDir &&
					assert.ObjectsAreEqual([]string{"*.db", "*.sqlite", "*.sqlite3"}, o.Include)
			})).RunAndReturn(func(_ context.Context, o restic.RestoreOptions) error {
				tt.Given.restored(t, o.Target)
				return nil
			})
			if tt.Then.err == "" {
				m.pinger.EXPECT().Ping(mock.Anything, "verify", "").Return()
			} else {
				m.pinger.EXPECT().Ping(mock.Anything, "verify", "/fail").Return()
			}

			err := b.Verify(context.Background())

			if tt.Then.err != "" {
				assert.ErrorContains(t, err, tt.Then.err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.Then.out, out.String())
			left, readErr := os.ReadDir(b.TempDir)
			require.NoError(t, readErr)
			assert.Empty(t, left, "the restored folder is removed")
		})
	}
}

func TestVerifyRunsOnTheMainOnly(t *testing.T) {
	tests := map[string]struct {
		Given struct {
			snapshots []restic.Snapshot
			err       error
		}
		Then struct{ err string }
	}{
		"another machine": {
			Given: struct {
				snapshots []restic.Snapshot
				err       error
			}{snapshots: []restic.Snapshot{snapshot("other", "pi2", "2026-10-04 04:30")}},
			Then: struct{ err string }{"another machine is gorgon's main; its verification runs there"},
		},
		"unreadable": {
			Given: struct {
				snapshots []restic.Snapshot
				err       error
			}{err: errors.New("boom")},
			Then: struct{ err string }{"cannot read the backup repository to tell which machine is gorgon's main (boom); nothing was checked"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			b, m, _, _ := fixture(t)
			m.pinger.EXPECT().Ping(mock.Anything, "verify", "/start").Return()
			m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(tt.Given.snapshots, tt.Given.err)
			m.pinger.EXPECT().Ping(mock.Anything, "verify", "/fail").Return()

			assert.EqualError(t, b.Verify(context.Background()), tt.Then.err)
		})
	}
}

func TestTheVerifyCleanupIsShieldedFromSignals(t *testing.T) {
	b, m, _, _ := fixture(t)
	shielded := false
	b.Shield = func() func() {
		shielded = true
		return func() { shielded = false }
	}
	m.pinger.EXPECT().Ping(mock.Anything, "verify", "/start").Return()
	m.repository.EXPECT().Snapshots(mock.Anything, "gorgon").Return(nil, nil)
	m.repository.EXPECT().Unlock(mock.Anything).Return(errors.New("restic unlock failed (exit 1)"))
	m.pinger.EXPECT().Ping(mock.Anything, "verify", "/fail").RunAndReturn(func(context.Context, string, string) {
		assert.True(t, shielded, "signals are shielded while the failure is reported")
	})

	assert.EqualError(t, b.Verify(context.Background()), "restic unlock failed (exit 1)")
	assert.False(t, shielded)
}
