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
		"--property=Id,LoadState,Result,ExecMainExitTimestamp,NextElapseUSecRealtime"}, c.Args)
	assert.Equal(s.t, []string{"TZ=UTC"}, c.Env)
	return process.Result{Stdout: []byte(s.stdout)}, s.err
}

func shown(blocks ...string) string { return strings.Join(blocks, "\n\n") + "\n" }

func service(job, result, exited string) string {
	return "Result=" + result + "\nExecMainExitTimestamp=" + exited + "\nId=mse-home-" + job + ".service\nLoadState=loaded"
}

func timer(job, next string) string {
	return "NextElapseUSecRealtime=" + next + "\nResult=success\nId=mse-home-" + job + ".timer\nLoadState=loaded"
}

func missing(job string) string {
	return "Result=success\nExecMainExitTimestamp=\nId=mse-home-" + job + ".service\nLoadState=not-found\n\n" +
		"NextElapseUSecRealtime=\nResult=success\nId=mse-home-" + job + ".timer\nLoadState=not-found"
}

func TestStatus(t *testing.T) {
	ran := time.Date(2026, 10, 7, 3, 31, 41, 0, time.UTC)
	next := time.Date(2026, 10, 8, 3, 30, 0, 0, time.UTC)
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
		"a main's four timers, one failed": {
			Given: Given{stdout: shown(
				service("update", "success", "Wed 2026-10-07 03:31:41 UTC"), timer("update", "Thu 2026-10-08 03:30:00 UTC"),
				service("backup", "exit-code", "Wed 2026-10-07 03:31:41 UTC"), timer("backup", "Thu 2026-10-08 03:30:00 UTC"),
				service("verify", "success", ""), timer("verify", "Thu 2026-10-08 03:30:00 UTC"),
				service("download-cleanup", "success", "Wed 2026-10-07 03:31:41 UTC"), timer("download-cleanup", "Thu 2026-10-08 03:30:00 UTC"))},
			Then: Then{statuses: []JobStatus{
				{Job: Update, Result: "success", LastRun: ran, Next: next},
				{Job: Backup, Result: "exit-code", LastRun: ran, Next: next},
				{Job: Verify, Result: "success", Next: next},
				{Job: Cleanup, Result: "success", LastRun: ran, Next: next},
			}},
		},
		"a copy without backup timers, and a failure with no exit time": {
			Given: Given{stdout: shown(
				service("update", "start-limit-hit", ""), timer("update", "Thu 2026-10-08 03:30:00 UTC"),
				missing("backup"), missing("verify"),
				service("download-cleanup", "success", "Wed 2026-10-07 03:31:41 UTC"), timer("download-cleanup", "Thu 2026-10-08 03:30:00 UTC"))},
			Then: Then{statuses: []JobStatus{
				{Job: Update, Result: "start-limit-hit", Next: next},
				{Job: Cleanup, Result: "success", LastRun: ran, Next: next},
			}},
		},
		"a time systemd printed in an unexpected form": {
			Given: Given{stdout: shown(
				service("update", "success", "2026-10-07T03:31:41Z"), timer("update", "Thu 2026-10-08 03:30:00 UTC"),
				missing("backup"), missing("verify"), missing("download-cleanup"))},
			Then: Then{err: `read the timers from systemd: mse-home-update.service's ExecMainExitTimestamp "2026-10-07T03:31:41Z" isn't a time`},
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
