package timers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/process"
)

type showRunner struct {
	t      *testing.T
	stdout string
	err    error
}

func (s showRunner) Output(_ context.Context, c process.Command) (process.Result, error) {
	assert.Equal(s.t, "systemctl", c.Name)
	assert.Equal(s.t, []string{"--user", "show", "mse-home-backup.service", "mse-home-backup.timer",
		"--property=Id,LoadState,Result,ExecMainExitTimestamp,NextElapseUSecRealtime"}, c.Args)
	return process.Result{Stdout: []byte(s.stdout)}, s.err
}

func TestStatus(t *testing.T) {
	london, err := time.LoadLocation("Europe/London")
	require.NoError(t, err)
	type Given struct {
		stdout string
		err    error
	}
	type Then struct {
		status JobStatus
		err    string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"ran and succeeded": {
			Given: Given{stdout: "Result=success\nExecMainExitTimestamp=Wed 2026-10-07 04:31:41 BST\nId=mse-home-backup.service\nLoadState=loaded\n\n" +
				"NextElapseUSecRealtime=Thu 2026-10-08 04:30:00 BST\nResult=success\nId=mse-home-backup.timer\nLoadState=loaded\n"},
			Then: Then{status: JobStatus{Installed: true, Succeeded: true,
				LastRun: time.Date(2026, 10, 7, 4, 31, 41, 0, london), Next: time.Date(2026, 10, 8, 4, 30, 0, 0, london)}},
		},
		"ran and failed": {
			Given: Given{stdout: "Result=exit-code\nExecMainExitTimestamp=Wed 2026-10-07 04:31:41 BST\nId=mse-home-backup.service\nLoadState=loaded\n\n" +
				"NextElapseUSecRealtime=Thu 2026-10-08 04:30:00 BST\nResult=success\nId=mse-home-backup.timer\nLoadState=loaded\n"},
			Then: Then{status: JobStatus{Installed: true, Succeeded: false,
				LastRun: time.Date(2026, 10, 7, 4, 31, 41, 0, london), Next: time.Date(2026, 10, 8, 4, 30, 0, 0, london)}},
		},
		"installed, never ran": {
			Given: Given{stdout: "Result=success\nExecMainExitTimestamp=\nId=mse-home-backup.service\nLoadState=loaded\n\n" +
				"NextElapseUSecRealtime=Thu 2026-10-08 04:30:00 BST\nResult=success\nId=mse-home-backup.timer\nLoadState=loaded\n"},
			Then: Then{status: JobStatus{Installed: true, Succeeded: true, Next: time.Date(2026, 10, 8, 4, 30, 0, 0, london)}},
		},
		"not installed": {
			Given: Given{stdout: "Result=success\nExecMainExitTimestamp=\nId=mse-home-backup.service\nLoadState=not-found\n\n" +
				"NextElapseUSecRealtime=\nResult=success\nId=mse-home-backup.timer\nLoadState=not-found\n"},
			Then: Then{status: JobStatus{}},
		},
		"systemctl failing": {
			Given: Given{err: errors.New("Failed to connect to bus")},
			Then:  Then{err: "read mse-home-backup's state from systemd: Failed to connect to bus"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TZ", "Europe/London")

			status, err := Status(context.Background(), showRunner{t: t, stdout: tt.Given.stdout, err: tt.Given.err}, "home", Backup)

			if tt.Then.err != "" {
				assert.EqualError(t, err, tt.Then.err)
				return
			}
			require.NoError(t, err)
			assert.True(t, tt.Then.status.LastRun.Equal(status.LastRun), "last run %v", status.LastRun)
			assert.True(t, tt.Then.status.Next.Equal(status.Next), "next %v", status.Next)
			assert.Equal(t, tt.Then.status.Installed, status.Installed)
			assert.Equal(t, tt.Then.status.Succeeded, status.Succeeded)
		})
	}
}
