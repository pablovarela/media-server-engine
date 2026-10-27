package timers

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/jobs"
)

var gorgon = Values{Installation: "gorgon", Executable: "/home/pablo/.local/bin/mse", MediaSchedule: "Sun *-*-* 01:00:00"}

func TestTheServicesRunMse(t *testing.T) {
	tests := map[Job]string{
		Update: `[Unit]
Description=Bring the media server up to date with its config (gorgon)
After=mse-gorgon-backup-apps.service

[Service]
Type=oneshot
ExecStart=/home/pablo/.local/bin/mse update --apply
Nice=10
IOSchedulingClass=idle
`,
		BackupApps: `[Unit]
Description=Back up the media server's app state (gorgon)

[Service]
Type=oneshot
ExecStart=/home/pablo/.local/bin/mse backup --apps
Nice=10
IOSchedulingClass=idle
`,
		CheckBackup: `[Unit]
Description=Check the media server's backups can be restored (gorgon)
After=mse-gorgon-backup-apps.service

[Service]
Type=oneshot
ExecStart=/home/pablo/.local/bin/mse check-backup
Nice=10
IOSchedulingClass=idle
`,
		BackupMedia: `[Unit]
Description=Back up the media server's media (gorgon)

[Service]
Type=oneshot
ExecStart=/home/pablo/.local/bin/mse backup --media
Nice=10
IOSchedulingClass=idle
`,
		CleanDownloads: `[Unit]
Description=Remove downloads Sonarr or Radarr flagged as executables (gorgon)

[Service]
Type=oneshot
ExecStart=/home/pablo/.local/bin/mse clean-downloads
Nice=10
IOSchedulingClass=idle
`,
		SearchMissing: `[Unit]
Description=Ask Sonarr and Radarr to search for everything missing (gorgon)
After=mse-gorgon-update.service

[Service]
Type=oneshot
ExecStart=/home/pablo/.local/bin/mse search-missing
Nice=10
`,
		ForwardPort: `[Unit]
Description=Keep Deluge on the VPN's forwarded port (gorgon)

[Service]
Type=oneshot
ExecStart=/home/pablo/.local/bin/mse forward-port
Nice=10
`,
	}
	for job, want := range tests {
		t.Run(job.Name, func(t *testing.T) {
			text, err := Render(job, ".service", gorgon)

			require.NoError(t, err)
			assert.Equal(t, want, text)
		})
	}
}

func TestTheTimersKeepTheirSchedules(t *testing.T) {
	tests := map[Job]struct {
		schedule   string
		persistent bool
	}{
		Update:         {"OnCalendar=*-*-* 05:00:00", true},
		BackupApps:     {"OnCalendar=*-*-* 04:30:00", true},
		CheckBackup:    {"OnCalendar=Sun *-*-* 05:30:00", true},
		CleanDownloads: {"OnCalendar=*:0/15", false},
		ForwardPort:    {"OnCalendar=*:0/15", false},
		SearchMissing:  {"OnCalendar=*-*-* 06:00:00", true},
		BackupMedia:    {"OnCalendar=Sun *-*-* 01:00:00", true},
	}
	for job, tt := range tests {
		t.Run(job.Name, func(t *testing.T) {
			text, err := Render(job, ".timer", gorgon)

			require.NoError(t, err)
			assert.Contains(t, text, tt.schedule+"\n")
			assert.Contains(t, text, "(gorgon)\n")
			assert.Equal(t, tt.persistent, strings.Contains(text, "Persistent=true\n"))
			assert.Contains(t, text, "WantedBy=timers.target\n")
		})
	}
}

func TestTheUnitsAreNamedForTheInstallation(t *testing.T) {
	assert.Equal(t, "mse-gorgon-clean-downloads", CleanDownloads.Unit("gorgon"))
	assert.Equal(t, []string{"mse-gorgon-backup-apps.service", "mse-gorgon-backup-apps.timer"}, BackupApps.Files("gorgon"))
}

func TestNoUnitLeavesAPlaceholder(t *testing.T) {
	for _, job := range []Job{Update, CleanDownloads, ForwardPort, SearchMissing, BackupApps, CheckBackup, BackupMedia} {
		for _, suffix := range []string{".service", ".timer"} {
			text, err := Render(job, suffix, gorgon)

			require.NoError(t, err)
			assert.NotContains(t, text, "@", job.Name+suffix)
		}
	}
}

func TestTheEnvironmentTheInstallationNeedsReachesTheServices(t *testing.T) {
	v := gorgon
	v.Environment = []string{"XDG_DATA_HOME=/mnt/ssd/share", `SOPS_AGE_KEY_CMD=op read "op://vault/age key" --at 100% \n`}

	text, err := Render(BackupApps, ".service", v)

	require.NoError(t, err)
	assert.Contains(t, text, "Type=oneshot\n"+
		`Environment="XDG_DATA_HOME=/mnt/ssd/share"`+"\n"+
		`Environment="SOPS_AGE_KEY_CMD=op read \"op://vault/age key\" --at 100%% \\n"`+"\n"+
		"ExecStart=")
}

func TestWhichTimersEachMachineRuns(t *testing.T) {
	tests := map[string]struct {
		role          Role
		media         bool
		install, keep []Job
	}{
		"main":                 {role: Main, install: []Job{Update, CleanDownloads, SearchMissing, BackupApps, CheckBackup}},
		"main with media":      {role: Main, media: true, install: []Job{Update, CleanDownloads, SearchMissing, BackupApps, CheckBackup, BackupMedia}},
		"secondary":            {role: Secondary, install: []Job{Update, CleanDownloads, SearchMissing}},
		"secondary with media": {role: Secondary, media: true, install: []Job{Update, CleanDownloads, SearchMissing}},
		"unknown":              {role: Unknown, media: true, install: []Job{Update, CleanDownloads, SearchMissing}, keep: []Job{BackupApps, CheckBackup, BackupMedia}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			install, keep := Plan(tt.role, jobs.Features{Media: tt.media})

			assert.Equal(t, tt.install, install)
			assert.Equal(t, tt.keep, keep)
		})
	}
}
