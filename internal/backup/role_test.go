package backup

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/restic"
)

func TestDescribeRole(t *testing.T) {
	type Given struct {
		snapshots []restic.Snapshot
		err       error
	}
	type Then struct {
		out string
		err string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"this machine": {
			Given: Given{snapshots: []restic.Snapshot{snapshot("this", "pi", "2026-10-05 04:30")}},
			Then:  Then{out: "gorgon's main is pi, last backup 2026-10-05 04:30.\nThis machine is the main.\n"},
		},
		"another machine": {
			Given: Given{snapshots: []restic.Snapshot{snapshot("other", "pi2", "2026-10-05 04:30")}},
			Then:  Then{out: "gorgon's main is pi2, last backup 2026-10-05 04:30.\nThis machine is not the main; mse claim-backup-main makes it the main.\n"},
		},
		"latest by time": {
			Given: Given{snapshots: []restic.Snapshot{snapshot("other", "pi2", "2026-10-05 04:30"), snapshot("this", "pi", "2026-10-03 04:30")}},
			Then:  Then{out: "gorgon's main is pi2, last backup 2026-10-05 04:30.\nThis machine is not the main; mse claim-backup-main makes it the main.\n"},
		},
		"no name tag": {
			Given: Given{snapshots: []restic.Snapshot{{Time: snapshot("x", "x", "2026-10-05 04:30").Time, Tags: []string{"machine:other"}}}},
			Then:  Then{out: "gorgon's main is machine other, last backup 2026-10-05 04:30.\nThis machine is not the main; mse claim-backup-main makes it the main.\n"},
		},
		"no backups yet": {
			Then: Then{out: "gorgon has no backups yet; the first machine to back up becomes its main.\n"},
		},
		"unreadable": {
			Given: Given{err: errors.New("restic snapshots failed (exit 1)")},
			Then:  Then{err: "cannot read the backup repository to tell which machine is gorgon's main (restic snapshots failed (exit 1))"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			b, m, out, _ := fixture(t)
			m.repository.EXPECT().Snapshots(context.Background(), "gorgon").Return(tt.Given.snapshots, tt.Given.err)

			err := b.DescribeRole(context.Background())

			if tt.Then.err != "" {
				assert.EqualError(t, err, tt.Then.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.Then.out, out.String())
		})
	}
}

func TestRunsBackups(t *testing.T) {
	type Given struct {
		snapshots []restic.Snapshot
		marked    bool
	}
	type Then struct {
		runs     bool
		backedUp bool
		marked   bool
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"this machine made the latest backup":        {Given: Given{snapshots: []restic.Snapshot{snapshot("this", "pi", "2026-10-05 04:30")}}, Then: Then{runs: true, backedUp: true, marked: true}},
		"another machine made the latest backup":     {Given: Given{snapshots: []restic.Snapshot{snapshot("other", "pi2", "2026-10-05 04:30")}, marked: true}, Then: Then{runs: false, backedUp: true, marked: false}},
		"no backups yet and this machine claimed it": {Given: Given{marked: true}, Then: Then{runs: true, marked: true}},
		"no backups yet and nothing claimed":         {Then: Then{runs: false, marked: false}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			b, m, _, _ := fixture(t)
			if tt.Given.marked {
				require.NoError(t, markMain(b.Installation.Data))
			}
			m.repository.EXPECT().Snapshots(context.Background(), "gorgon").Return(tt.Given.snapshots, nil)

			runs, backedUp, err := b.RunsBackups(context.Background())

			require.NoError(t, err)
			assert.Equal(t, tt.Then.runs, runs)
			assert.Equal(t, tt.Then.backedUp, backedUp)
			assert.Equal(t, tt.Then.marked, b.Installation.Role() == "main")
		})
	}
}

func TestRunsBackupsNeedsTheRepository(t *testing.T) {
	b, m, _, _ := fixture(t)
	require.NoError(t, markMain(b.Installation.Data))
	m.repository.EXPECT().Snapshots(context.Background(), "gorgon").Return(nil, errors.New("restic snapshots failed (exit 1)"))

	_, _, err := b.RunsBackups(context.Background())

	assert.EqualError(t, err, "cannot read the backup repository to tell which machine is gorgon's main (restic snapshots failed (exit 1))")
	assert.Equal(t, "main", b.Installation.Role())
}

func TestCurrentMain(t *testing.T) {
	at := func(s string) time.Time { when, _ := time.Parse("2006-01-02 15:04", s); return when }
	tests := map[string]struct {
		snapshots []restic.Snapshot
		main      *Main
	}{
		"another machine": {snapshots: []restic.Snapshot{snapshot("other", "pi2", "2026-10-05 04:30"), snapshot("this", "pi", "2026-10-03 04:30")},
			main: &Main{Machine: "pi2", Time: at("2026-10-05 04:30")}},
		"this machine": {snapshots: []restic.Snapshot{snapshot("this", "pi", "2026-10-05 04:30")},
			main: &Main{Machine: "pi", Time: at("2026-10-05 04:30"), ThisMachine: true}},
		"no name tag": {snapshots: []restic.Snapshot{{Time: at("2026-10-05 04:30"), Tags: []string{"machine:other"}}},
			main: &Main{Machine: "machine other", Time: at("2026-10-05 04:30")}},
		"no backups": {},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			b, m, _, _ := fixture(t)
			m.repository.EXPECT().Snapshots(context.Background(), "gorgon").Return(tt.snapshots, nil)

			main, err := b.CurrentMain(context.Background())

			require.NoError(t, err)
			assert.Equal(t, tt.main, main)
		})
	}
}

func TestCurrentMainWhenTheRepositoryCannotBeRead(t *testing.T) {
	b, m, _, _ := fixture(t)
	m.repository.EXPECT().Snapshots(context.Background(), "gorgon").Return(nil, errors.New("restic snapshots failed (exit 1)"))

	_, err := b.CurrentMain(context.Background())

	assert.EqualError(t, err, "cannot read the backup repository to tell which machine is gorgon's main (restic snapshots failed (exit 1))")
}
