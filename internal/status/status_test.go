package status

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/pablovarela/media-server-engine/internal/backup"
	"github.com/pablovarela/media-server-engine/internal/runs"
)

var now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func healthy() Report {
	return Report{
		Installation: "home", Version: "v0.23.0",
		Main:    &backup.Main{Machine: "pi", Time: now.Add(-7 * time.Hour), ThisMachine: true},
		Running: 12, Services: 12,
		Timers: []Timer{
			{Label: "update", Next: now.Add(17 * time.Hour), Last: runs.Run{Started: now.Add(-7 * time.Hour)}, State: runs.Succeeded, Result: "success"},
			{Label: "backup", Next: now.Add(16 * time.Hour), Last: runs.Run{Started: now.Add(-7 * time.Hour)}, State: runs.Succeeded, Result: "success"},
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
				"Stack: 12 of 12 services running", "Last apps backup: 7 Oct 05:00 by pi",
				"update", "last ran 7 Oct 05:00, succeeded", "next 8 Oct 05:00", "Jellyfin", "http://media.local:8096"},
		},
		"another machine is the main": {
			Given: func(r *Report) { r.Main = &backup.Main{Machine: "laptop", Time: now.Add(-time.Hour)}; r.Timers = nil },
			Then:  []string{"laptop is the main: it made the latest backup. This machine doesn't back up; mse backup --apps --take-over makes it the main."},
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
		"a failed run": {
			Given: func(r *Report) { r.Timers[1].State = runs.Failed },
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
		"a run that stopped before finishing": {
			Given: func(r *Report) { r.Timers[0].State = runs.Interrupted },
			Then:  []string{"started 7 Oct 05:00, stopped before finishing"},
		},
		"running now": {
			Given: func(r *Report) {
				r.Timers[1].Last, r.Timers[1].State, r.Timers[1].Next = runs.Run{Started: now.Add(-time.Minute)}, runs.Running, time.Time{}
			},
			Then: []string{"running now, since 7 Oct 11:59"},
		},
		"no run recorded": {
			Given: func(r *Report) { r.Timers[0].Last, r.Timers[0].State = runs.Run{}, runs.NeverRan },
			Then:  []string{"hasn't run yet"},
		},
		"systemd couldn't run it after the last record": {
			Given: func(r *Report) { r.Timers[0].Result, r.Timers[0].Stopped = "start-limit-hit", now.Add(-time.Hour) },
			Then:  []string{"systemd couldn't run it (start-limit-hit), 7 Oct 11:00"},
		},
		"an unreadable record": {
			Given: func(r *Report) { r.Timers[0].Err = errors.New("unexpected end of JSON input") },
			Then:  []string{"couldn't read its last run: unexpected end of JSON input", "last ran 7 Oct 05:00, succeeded"},
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
		"a failed run":                   {Given: func(r *Report) { r.Timers[1].State = runs.Failed }, Then: []string{"backup's last run failed"}},
		"a run that stopped unfinished":  {Given: func(r *Report) { r.Timers[0].State = runs.Interrupted }, Then: []string{"update's last run stopped before finishing"}},
		"running now is fine":            {Given: func(r *Report) { r.Timers[1].State = runs.Running }},
		"a timer that never ran is fine": {Given: func(r *Report) { r.Timers[1].Last, r.Timers[1].State = runs.Run{}, runs.NeverRan }},
		"systemd couldn't run it": {
			Given: func(r *Report) { r.Timers[0].Result, r.Timers[0].Stopped = "exit-code", now.Add(-time.Hour) },
			Then:  []string{"systemd couldn't run update (exit-code)"},
		},
		"a systemd failure older than the last run is past": {
			Given: func(r *Report) { r.Timers[0].Result, r.Timers[0].Stopped = "exit-code", now.Add(-8*time.Hour) },
		},
		"a systemd failure mse recorded already counts once": {
			Given: func(r *Report) {
				r.Timers[1].State, r.Timers[1].Result, r.Timers[1].Stopped = runs.Failed, "exit-code", now
			},
			Then: []string{"backup's last run failed"},
		},
		"an unreadable record":       {Given: func(r *Report) { r.Timers[0].Err = errors.New("bad") }, Then: []string{"update's last run couldn't be read"}},
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

func TestTheMediaBackupLine(t *testing.T) {
	tests := map[string]struct {
		media *MediaBackup
		line  string
		needs string
	}{
		"off":             {line: ""},
		"never backed up": {media: &MediaBackup{}, line: "Last media backup: none yet\n"},
		"backed up":       {media: &MediaBackup{Latest: time.Date(2026, 10, 4, 1, 40, 0, 0, time.UTC)}, line: "Last media backup: 4 Oct 01:40\n"},
		"unreadable": {
			media: &MediaBackup{Err: errors.New("restic snapshots failed (exit 1)")},
			line:  "Last media backup: couldn't read it: restic snapshots failed (exit 1)\n", needs: "the media backup repository couldn't be read",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := healthy()
			r.Media = tt.media

			out := r.Render(now)

			if tt.line == "" {
				assert.NotContains(t, out, "media backup")
			} else {
				assert.Contains(t, out, tt.line)
			}
			if tt.needs != "" {
				assert.Contains(t, r.Attention(now), tt.needs)
			} else {
				assert.Empty(t, r.Attention(now))
			}
		})
	}
}
