package timers

import (
	"context"
	"errors"
	"strings"
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
	assert.Equal(s.t, []string{"--user", "show",
		"mse-home-update.service", "mse-home-update.timer", "mse-home-backup.service", "mse-home-backup.timer",
		"mse-home-verify.service", "mse-home-verify.timer", "mse-home-download-cleanup.service", "mse-home-download-cleanup.timer",
		"--property=Id,LoadState,ActiveState,Result,LastTriggerUSec,NextElapseUSecRealtime"}, c.Args)
	assert.Equal(s.t, []string{"TZ=UTC"}, c.Env)
	return process.Result{Stdout: []byte(s.stdout)}, s.err
}

func shown(blocks ...string) string { return strings.Join(blocks, "\n\n") + "\n" }

func service(job, state, result string) string {
	return "ActiveState=" + state + "\nResult=" + result + "\nId=mse-home-" + job + ".service\nLoadState=loaded"
}

func timer(job, triggered, next string) string {
	return "LastTriggerUSec=" + triggered + "\nNextElapseUSecRealtime=" + next + "\nResult=success\nId=mse-home-" + job + ".timer\nLoadState=loaded"
}

func missing(job string) string {
	return "ActiveState=inactive\nResult=success\nId=mse-home-" + job + ".service\nLoadState=not-found\n\n" +
		"LastTriggerUSec=\nNextElapseUSecRealtime=\nResult=success\nId=mse-home-" + job + ".timer\nLoadState=not-found"
}

func TestStatus(t *testing.T) {
	ran := time.Date(2026, 10, 7, 3, 30, 58, 0, time.UTC)
	next := time.Date(2026, 10, 8, 3, 30, 0, 0, time.UTC)
	const triggered, upcoming = "Wed 2026-10-07 03:30:58 UTC", "Thu 2026-10-08 03:30:00 UTC"
	type Given struct {
		stdout string
		err    error
	}
	type Then struct {
		statuses []JobStatus
		err      string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"a main's four timers: one failed, one never triggered": {
			Given: Given{stdout: shown(
				service("update", "inactive", "success"), timer("update", triggered, upcoming),
				service("backup", "inactive", "exit-code"), timer("backup", triggered, upcoming),
				service("verify", "inactive", "success"), timer("verify", "", upcoming),
				service("download-cleanup", "inactive", "success"), timer("download-cleanup", triggered, upcoming))},
			Then: Then{statuses: []JobStatus{
				{Job: Update, Result: "success", LastRun: ran, Next: next},
				{Job: Backup, Result: "exit-code", LastRun: ran, Next: next},
				{Job: Verify, Result: "success", Next: next},
				{Job: Cleanup, Result: "success", LastRun: ran, Next: next},
			}},
		},
		"running now, with no next run until it ends": {
			Given: Given{stdout: shown(
				service("update", "inactive", "success"), timer("update", triggered, upcoming),
				missing("backup"), missing("verify"),
				service("download-cleanup", "activating", "success"), timer("download-cleanup", triggered, ""))},
			Then: Then{statuses: []JobStatus{
				{Job: Update, Result: "success", LastRun: ran, Next: next},
				{Job: Cleanup, Result: "success", Running: true, LastRun: ran},
			}},
		},
		"a time systemd printed in an unexpected form": {
			Given: Given{stdout: shown(
				service("update", "inactive", "success"), timer("update", "2026-10-07T03:30:58Z", upcoming),
				missing("backup"), missing("verify"), missing("download-cleanup"))},
			Then: Then{err: `read the timers from systemd: mse-home-update.timer's LastTriggerUSec "2026-10-07T03:30:58Z" isn't a time`},
		},
		"systemctl failing": {
			Given: Given{err: errors.New("Failed to connect to bus")},
			Then:  Then{err: "read the timers from systemd: Failed to connect to bus"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			statuses, err := Status(context.Background(), showRunner{t: t, stdout: tt.Given.stdout, err: tt.Given.err}, "home")

			if tt.Then.err != "" {
				assert.EqualError(t, err, tt.Then.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.Then.statuses, statuses)
		})
	}
}

func TestJobStatusSucceeded(t *testing.T) {
	assert.True(t, JobStatus{Result: "success"}.Succeeded())
	assert.False(t, JobStatus{Result: "start-limit-hit"}.Succeeded())
}
