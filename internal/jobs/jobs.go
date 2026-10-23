package jobs

const (
	Update         = "update"
	BackupApps     = "backup-apps"
	BackupMedia    = "backup-media"
	CheckBackup    = "check-backup"
	CleanDownloads = "clean-downloads"
)

func Unit(installation, job string) string {
	return "mse-" + installation + "-" + job
}

func OnEveryMachine(job string) bool {
	return job == Update || job == CleanDownloads
}
