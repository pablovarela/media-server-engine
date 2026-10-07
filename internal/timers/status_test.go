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
		"mse-home-verify.service", "mse-home-verify.timer", "mse-home-media-backup.service", "mse-home-media-backup.timer", "mse-home-download-cleanup.service", "mse-home-download-cleanup.timer",
		"--property=Id,LoadState,Result,InactiveEnterTimestamp,NextElapseUSecRealtime"}, c.Args)
	assert.Equal(s.t, []string{"TZ=UTC"}, c.Env)
	return process.Result{Stdout: []byte(s.stdout)}, s.err
}

func shown(blocks ...string) string { return strings.Join(blocks, "\n\n") + "\n" }

func service(job string) string {
	return failedService(job, "success", "")
}

func failedService(job, result, stopped string) string {
	return "Result=" + result + "\nInactiveEnterTimestamp=" + stopped + "\nId=mse-home-" + job + ".service\nLoadState=loaded"
}

func timer(job, next string) string {
	return "NextElapseUSecRealtime=" + next + "\nId=mse-home-" + job + ".timer\nLoadState=loaded"
}

func missing(job string) string {
	return "Id=mse-home-" + job + ".service\nLoadState=not-found\n\nNextElapseUSecRealtime=\nId=mse-home-" + job + ".timer\nLoadState=not-found"
}

func TestStatus(t *testing.T) {
	next := time.Date(2026, 10, 8, 3, 30, 0, 0, time.UTC)
	const upcoming = "Thu 2026-10-08 03:30:00 UTC"
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
		"a main's five timers": {
			Given: Given{stdout: shown(
				service("update"), timer("update", upcoming), service("backup"), timer("backup", upcoming),
				service("verify"), timer("verify", upcoming), service("media-backup"), timer("media-backup", upcoming),
				service("download-cleanup"), timer("download-cleanup", upcoming))},
			Then: Then{statuses: []JobStatus{
				{Job: Update, Next: next, Result: "success"}, {Job: Backup, Next: next, Result: "success"},
				{Job: Verify, Next: next, Result: "success"}, {Job: MediaBackup, Next: next, Result: "success"},
				{Job: Cleanup, Next: next, Result: "success"},
			}},
		},
		"a service systemd couldn't run": {
			Given: Given{stdout: shown(
				failedService("update", "start-limit-hit", "Wed 2026-10-07 04:00:02 UTC"), timer("update", upcoming),
				missing("backup"), missing("verify"), missing("media-backup"), missing("download-cleanup"))},
			Then: Then{statuses: []JobStatus{{Job: Update, Next: next, Result: "start-limit-hit", Stopped: time.Date(2026, 10, 7, 4, 0, 2, 0, time.UTC)}}},
		},
		"a copy without backup timers, one running with no next run until it ends": {
			Given: Given{stdout: shown(
				service("update"), timer("update", upcoming), missing("backup"), missing("verify"), missing("media-backup"),
				service("download-cleanup"), timer("download-cleanup", ""))},
			Then: Then{statuses: []JobStatus{{Job: Update, Next: next, Result: "success"}, {Job: Cleanup, Result: "success"}}},
		},
		"a time systemd printed in an unexpected form": {
			Given: Given{stdout: shown(
				service("update"), timer("update", "2026-10-08T03:30:00Z"), missing("backup"), missing("verify"), missing("media-backup"), missing("download-cleanup"))},
			Then: Then{err: `read the timers from systemd: mse-home-update.timer's NextElapseUSecRealtime "2026-10-08T03:30:00Z" isn't a time`},
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
