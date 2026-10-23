package jobs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnitsAreNamedAfterTheInstallationAndTheJob(t *testing.T) {
	tests := map[string]string{
		Update:         "mse-gorgon-update",
		BackupApps:     "mse-gorgon-backup-apps",
		BackupMedia:    "mse-gorgon-backup-media",
		CheckBackup:    "mse-gorgon-check-backup",
		CleanDownloads: "mse-gorgon-clean-downloads",
	}
	for job, want := range tests {
		t.Run(job, func(t *testing.T) {
			assert.Equal(t, want, Unit("gorgon", job))
		})
	}
}

func TestOnlyUpdateAndCleanDownloadsRunOnEveryMachine(t *testing.T) {
	tests := map[string]bool{
		Update:         true,
		CleanDownloads: true,
		BackupApps:     false,
		BackupMedia:    false,
		CheckBackup:    false,
	}
	for job, want := range tests {
		t.Run(job, func(t *testing.T) {
			assert.Equal(t, want, OnEveryMachine(job))
		})
	}
}

func TestWhichJobsAMachineRuns(t *testing.T) {
	tests := map[string]struct {
		main, media bool
		want        []string
	}{
		"main":            {main: true, want: []string{Update, CleanDownloads, BackupApps, CheckBackup}},
		"main with media": {main: true, media: true, want: []string{Update, CleanDownloads, BackupApps, CheckBackup, BackupMedia}},
		"other machine":   {media: true, want: []string{Update, CleanDownloads}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, Planned(tt.main, tt.media))
		})
	}
}

func TestEveryJobHasALabelForMseStatus(t *testing.T) {
	assert.Equal(t, []string{Update, CleanDownloads, BackupApps, CheckBackup, BackupMedia}, Names())
	labels := map[string]string{}
	for _, job := range Names() {
		labels[job] = Label(job)
	}
	assert.Equal(t, map[string]string{
		Update: "update", CleanDownloads: "clean-downloads", BackupApps: "apps backup", CheckBackup: "check-backup", BackupMedia: "media backup",
	}, labels)
}
