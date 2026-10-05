package backup

import (
	"context"
	"errors"
	"testing"

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
