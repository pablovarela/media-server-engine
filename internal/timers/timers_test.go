package timers

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var gorgon = Values{Installation: "gorgon", Executable: "/home/pablo/.local/bin/mse"}

func TestTheServicesRunMse(t *testing.T) {
	tests := map[Job]string{
		Update: `[Unit]
Description=Bring the media server up to date with its config (gorgon)
After=mse-gorgon-backup.service

[Service]
Type=oneshot
Environment=MSE_INSTALLATION=gorgon
ExecStart=/home/pablo/.local/bin/mse update --apply
Nice=10
IOSchedulingClass=idle
`,
		Backup: `[Unit]
Description=Back up the media server's app state (gorgon)

[Service]
Type=oneshot
Environment=MSE_INSTALLATION=gorgon
ExecStart=/home/pablo/.local/bin/mse backup
Nice=10
IOSchedulingClass=idle
`,
		Verify: `[Unit]
Description=Check the media server's backups can be restored (gorgon)
After=mse-gorgon-backup.service

[Service]
Type=oneshot
Environment=MSE_INSTALLATION=gorgon
ExecStart=/home/pablo/.local/bin/mse verify-backup
Nice=10
IOSchedulingClass=idle
`,
		Cleanup: `[Unit]
Description=Remove downloads Sonarr or Radarr flagged as executables (gorgon)

[Service]
Type=oneshot
Environment=MSE_INSTALLATION=gorgon
ExecStart=/home/pablo/.local/bin/mse remove-executable-downloads
Nice=10
IOSchedulingClass=idle
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
		Update:  {"OnCalendar=*-*-* 05:00:00", true},
		Backup:  {"OnCalendar=*-*-* 04:30:00", true},
		Verify:  {"OnCalendar=Sun *-*-* 05:30:00", true},
		Cleanup: {"OnCalendar=*:0/15", false},
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
	assert.Equal(t, "mse-gorgon-download-cleanup", Cleanup.Unit("gorgon"))
	assert.Equal(t, []string{"mse-gorgon-backup.service", "mse-gorgon-backup.timer"}, Backup.Files("gorgon"))
}

func TestNoUnitLeavesAPlaceholder(t *testing.T) {
	for _, job := range []Job{Update, Cleanup, Backup, Verify} {
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

	text, err := Render(Backup, ".service", v)

	require.NoError(t, err)
	assert.Contains(t, text, "Environment=MSE_INSTALLATION=gorgon\n"+
		`Environment="XDG_DATA_HOME=/mnt/ssd/share"`+"\n"+
		`Environment="SOPS_AGE_KEY_CMD=op read \"op://vault/age key\" --at 100%% \\n"`+"\n"+
		"ExecStart=")
}

func TestWhichTimersEachMachineRuns(t *testing.T) {
	tests := map[Role]struct{ install, remove []Job }{
		Main:      {install: []Job{Update, Cleanup, Backup, Verify}},
		Secondary: {install: []Job{Update, Cleanup}, remove: []Job{Backup, Verify}},
		Unknown:   {install: []Job{Update, Cleanup}},
	}
	for role, want := range tests {
		install, remove := Plan(role)

		assert.Equal(t, want.install, install, role)
		assert.Equal(t, want.remove, remove, role)
	}
}
