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
