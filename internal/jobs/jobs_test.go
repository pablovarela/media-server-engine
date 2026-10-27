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
		ForwardPort:    "mse-gorgon-forward-port",
		SearchMissing:  "mse-gorgon-search-missing",
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
		ForwardPort:    true,
		SearchMissing:  true,
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
		main     bool
		features Features
		want     []string
	}{
		"main":                          {main: true, want: []string{Update, CleanDownloads, SearchMissing, BackupApps, CheckBackup}},
		"main with media":               {main: true, features: Features{Media: true}, want: []string{Update, CleanDownloads, SearchMissing, BackupApps, CheckBackup, BackupMedia}},
		"other machine":                 {features: Features{Media: true}, want: []string{Update, CleanDownloads, SearchMissing}},
		"main with port forwarding":     {main: true, features: Features{ForwardPort: true}, want: []string{Update, CleanDownloads, ForwardPort, SearchMissing, BackupApps, CheckBackup}},
		"other machine with forwarding": {features: Features{ForwardPort: true}, want: []string{Update, CleanDownloads, ForwardPort, SearchMissing}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, Planned(tt.main, tt.features))
		})
	}
}

func TestEveryJobHasALabelForMseStatus(t *testing.T) {
	assert.Equal(t, []string{Update, CleanDownloads, ForwardPort, SearchMissing, BackupApps, CheckBackup, BackupMedia}, Names())
	labels := map[string]string{}
	for _, job := range Names() {
		labels[job] = Label(job)
	}
	assert.Equal(t, map[string]string{
		Update: "update", CleanDownloads: "clean-downloads", ForwardPort: "port forwarding", SearchMissing: "missing search", BackupApps: "apps backup", CheckBackup: "check-backup", BackupMedia: "media backup",
	}, labels)
}
