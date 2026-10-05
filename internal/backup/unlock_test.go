package backup

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/restic"
)

func TestUnlock(t *testing.T) {
	type Given struct {
		all   bool
		locks []restic.Lock
	}
	type Then struct {
		out string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"stale locks only, none left": {Then: Then{out: "no locks left on the backup repository\n"}},
		"every lock":                  {Given: Given{all: true}, Then: Then{out: "no locks left on the backup repository\n"}},
		"locks left": {
			Given: Given{locks: []restic.Lock{{Exclusive: true, Hostname: "pi", PID: 42, Time: time.Date(2026, 10, 5, 4, 30, 0, 0, time.UTC)}}},
			Then: Then{out: "Locks left, held by restic processes that may still be running:\n" +
				"  exclusive lock from pi (process 42) since 2026-10-05 04:30\n" +
				"If none of those machines is running restic now, remove them with: mse unlock-backup --all\n"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			b, m, out, _ := fixture(t)
			if tt.Given.all {
				m.repository.EXPECT().UnlockAll(context.Background()).Return(nil)
			} else {
				m.repository.EXPECT().Unlock(context.Background()).Return(nil)
			}
			m.repository.EXPECT().Locks(context.Background()).Return(tt.Given.locks, nil)

			require.NoError(t, b.Unlock(context.Background(), tt.Given.all))

			assert.Equal(t, tt.Then.out, out.String())
		})
	}
}
