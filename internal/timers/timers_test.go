package timers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var gorgon = Values{User: "pablo", Group: "staff", Installation: "gorgon", Executable: "/home/pablo/.local/bin/mse"}

func TestTheServicesRunMse(t *testing.T) {
	tests := map[string]string{
		"media-update.service": `[Unit]
Description=Bring the media server up to date with its config repository
Wants=network-online.target
After=network-online.target docker.service media-backup.service

[Service]
Type=oneshot
User=pablo
Group=staff
SupplementaryGroups=docker
Environment=MSE_INSTALLATION=gorgon
ExecStart=/home/pablo/.local/bin/mse update --apply
Nice=10
IOSchedulingClass=idle
`,
		"media-backup.service": `[Unit]
Description=Back up media-server app state
Wants=network-online.target
After=network-online.target docker.service

[Service]
Type=oneshot
User=pablo
Group=staff
SupplementaryGroups=docker
Environment=MSE_INSTALLATION=gorgon
ExecStart=/home/pablo/.local/bin/mse backup
Nice=10
IOSchedulingClass=idle
`,
		"media-verify.service": `[Unit]
Description=Verify the latest media-server backup restores cleanly
Wants=network-online.target
After=network-online.target media-backup.service

[Service]
Type=oneshot
User=pablo
Group=staff
Environment=MSE_INSTALLATION=gorgon
ExecStart=/home/pablo/.local/bin/mse verify-backup
Nice=10
IOSchedulingClass=idle
`,
		"media-download-cleanup.service": `[Unit]
Description=Remove downloads that Sonarr or Radarr flagged as executables
After=docker.service

[Service]
Type=oneshot
User=pablo
Group=staff
Environment=MSE_INSTALLATION=gorgon
ExecStart=/home/pablo/.local/bin/mse remove-executable-downloads
Nice=10
`,
	}
	for file, want := range tests {
		t.Run(file, func(t *testing.T) {
			text, err := Render(file, gorgon)

			require.NoError(t, err)
			assert.Equal(t, want, text)
		})
	}
}

func TestTheTimersKeepTheirSchedules(t *testing.T) {
	for file, schedule := range map[string]string{
		"media-update.timer":           "OnCalendar=*-*-* 05:00:00",
		"media-backup.timer":           "OnCalendar=*-*-* 04:30:00",
		"media-verify.timer":           "OnCalendar=Sun *-*-* 05:30:00",
		"media-download-cleanup.timer": "OnCalendar=*:0/15",
	} {
		t.Run(file, func(t *testing.T) {
			text, err := Render(file, gorgon)

			require.NoError(t, err)
			assert.Contains(t, text, schedule+"\n")
			assert.Contains(t, text, "WantedBy=timers.target\n")
		})
	}
}

func TestNoUnitLeavesAPlaceholder(t *testing.T) {
	for _, unit := range []Unit{Update, Cleanup, Backup, Verify} {
		for _, file := range unit.Files() {
			text, err := Render(file, gorgon)

			require.NoError(t, err)
			assert.NotContains(t, text, "@", file)
		}
	}
}

func TestAnUnknownFileIsAnError(t *testing.T) {
	_, err := Render("media-nothing.service", gorgon)

	assert.Error(t, err)
}

func TestTheMainRunsTheBackupsAndAnotherMachineRemovesThem(t *testing.T) {
	install, remove := Plan(true)
	assert.Equal(t, []Unit{Update, Cleanup, Backup, Verify}, install)
	assert.Empty(t, remove)

	install, remove = Plan(false)
	assert.Equal(t, []Unit{Update, Cleanup}, install)
	assert.Equal(t, []Unit{Backup, Verify}, remove)
}
