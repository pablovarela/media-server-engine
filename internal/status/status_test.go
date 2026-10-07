package status

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/pablovarela/media-server-engine/internal/backup"
	"github.com/pablovarela/media-server-engine/internal/timers"
)

var now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func healthy() Report {
	return Report{
		Installation: "home", Version: "v0.23.0",
		Main:    &backup.Main{Machine: "pi", Time: now.Add(-7 * time.Hour), ThisMachine: true},
		Running: 12, Services: 12,
		Timers: []Timer{
			{Label: "update", Status: timers.JobStatus{Result: "success", LastRun: now.Add(-7 * time.Hour), Next: now.Add(17 * time.Hour)}},
			{Label: "backup", Status: timers.JobStatus{Result: "success", LastRun: now.Add(-7 * time.Hour), Next: now.Add(16 * time.Hour)}},
		},
		Apps: []App{{Name: "Jellyfin", Address: "http://media.local:8096"}},
	}
}

func TestRender(t *testing.T) {
	tests := map[string]struct {
		Given func(r *Report)
		Then  []string
	}{
		"healthy main": {
			Given: func(*Report) {},
			Then: []string{"home on mse v0.23.0", "This machine is the main: it made the latest backup.",
				"Stack: 12 of 12 services running", "Last backup: 7 Oct 05:00 by pi",
				"update", "last ran 7 Oct 05:00, succeeded", "next 8 Oct 05:00", "Jellyfin", "http://media.local:8096"},
		},
		"another machine is the main": {
			Given: func(r *Report) { r.Main = &backup.Main{Machine: "laptop", Time: now.Add(-time.Hour)}; r.Timers = nil },
			Then:  []string{"laptop is the main: it made the latest backup. This machine doesn't back up; mse claim-backup-main makes it the main."},
		},
		"no backups yet": {
			Given: func(r *Report) { r.Main = nil },
			Then:  []string{"No backups yet; the first machine to back up becomes the main."},
		},
		"backup repository unreachable": {
			Given: func(r *Report) { r.Main, r.MainErr = nil, errors.New("dial tcp: no route to host") },
			Then:  []string{"Couldn't reach the backup repository: dial tcp: no route to host", "Stack: 12 of 12 services running"},
		},
		"no backup repository configured": {
			Given: func(r *Report) { r.Main, r.NoBackups = nil, true },
			Then:  []string{"No backup repository is set; mse configure sets one."},
		},
		"no systemd": {
			Given: func(r *Report) { r.Timers, r.NoSystemd = nil, true },
			Then:  []string{"No timers on this machine (no systemd)."},
		},
		"a failed timer": {
			Given: func(r *Report) { r.Timers[1].Status.Result = "exit-code" },
			Then:  []string{"last ran 7 Oct 05:00, failed"},
		},
		"stack unreadable": {
			Given: func(r *Report) { r.StackErr = errors.New("docker: permission denied") },
			Then:  []string{"Stack: couldn't read it: docker: permission denied"},
		},
		"no containers": {
			Given: func(r *Report) { r.Running, r.Services = 0, 0 },
			Then:  []string{"Stack: no containers; mse apply starts it"},
		},
		"a failure with no exit time": {
			Given: func(r *Report) {
				r.Timers[0].Status = timers.JobStatus{Result: "start-limit-hit", Next: now.Add(time.Hour)}
			},
			Then: []string{"last run failed (start-limit-hit)"},
		},
		"a timer that hasn't run": {
			Given: func(r *Report) { r.Timers[0].Status.LastRun = time.Time{} },
			Then:  []string{"hasn't run yet"},
		},
		"apps unreadable": {
			Given: func(r *Report) { r.Apps, r.AppsErr = nil, errors.New("no hostname") },
			Then:  []string{"Apps: couldn't list them: no hostname", "Stack: 12 of 12 services running"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := healthy()
			tt.Given(&r)

			out := r.Render(now)

			for _, line := range tt.Then {
				assert.Contains(t, out, line)
			}
		})
	}
}

func TestAttention(t *testing.T) {
	tests := map[string]struct {
		Given func(r *Report)
		Then  []string
	}{
		"nothing":                        {Given: func(*Report) {}},
		"a failed timer":                 {Given: func(r *Report) { r.Timers[1].Status.Result = "exit-code" }, Then: []string{"backup's last run failed"}},
		"a timer that never ran is fine": {Given: func(r *Report) { r.Timers[1].Status.LastRun = time.Time{} }},
		"a failure with no exit time": {
			Given: func(r *Report) { r.Timers[0].Status = timers.JobStatus{Result: "start-limit-hit"} }, Then: []string{"update's last run failed"},
		},
		"no containers":              {Given: func(r *Report) { r.Running, r.Services = 0, 0 }, Then: []string{"the stack isn't running"}},
		"timers unreadable":          {Given: func(r *Report) { r.Timers, r.TimersErr = nil, errors.New("no bus") }, Then: []string{"the timers couldn't be read"}},
		"apps unreadable alone":      {Given: func(r *Report) { r.Apps, r.AppsErr = nil, errors.New("no hostname") }},
		"a service down":             {Given: func(r *Report) { r.Running = 11 }, Then: []string{"1 of 12 services isn't running"}},
		"two services down":          {Given: func(r *Report) { r.Running = 10 }, Then: []string{"2 of 12 services aren't running"}},
		"the main's backup is stale": {Given: func(r *Report) { r.Main.Time = now.Add(-49 * time.Hour) }, Then: []string{"the latest backup is more than 2 days old"}},
		"another main's stale backup is not this machine's concern": {
			Given: func(r *Report) { r.Main = &backup.Main{Machine: "laptop", Time: now.Add(-100 * time.Hour)} },
		},
		"unreachable repository":   {Given: func(r *Report) { r.Main, r.MainErr = nil, errors.New("offline") }, Then: []string{"the backup repository couldn't be read"}},
		"no backup repository set": {Given: func(r *Report) { r.Main, r.NoBackups = nil, true }},
		"stack unreadable":         {Given: func(r *Report) { r.StackErr = errors.New("docker down") }, Then: []string{"the stack couldn't be read"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := healthy()
			tt.Given(&r)

			assert.Equal(t, tt.Then, r.Attention(now))
		})
	}
}
