package jobs

const (
	Update         = "update"
	BackupApps     = "backup-apps"
	BackupMedia    = "backup-media"
	CheckBackup    = "check-backup"
	CleanDownloads = "clean-downloads"
)

var All = []string{Update, BackupApps, CheckBackup, BackupMedia, CleanDownloads}

func Unit(installation, job string) string {
	return "mse-" + installation + "-" + job
}

func OnEveryMachine(job string) bool {
	return job == Update || job == CleanDownloads
}
