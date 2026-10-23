package jobs

const (
	Update         = "update"
	BackupApps     = "backup-apps"
	BackupMedia    = "backup-media"
	CheckBackup    = "check-backup"
	CleanDownloads = "clean-downloads"
)

type job struct {
	name     string
	label    string
	mainOnly bool
	media    bool
}

var table = []job{
	{name: Update, label: "update"},
	{name: CleanDownloads, label: "clean-downloads"},
	{name: BackupApps, label: "apps backup", mainOnly: true},
	{name: CheckBackup, label: "check-backup", mainOnly: true},
	{name: BackupMedia, label: "media backup", mainOnly: true, media: true},
}

func Names() []string {
	names := make([]string, 0, len(table))
	for _, j := range table {
		names = append(names, j.name)
	}
	return names
}

func Planned(main, media bool) []string {
	var names []string
	for _, j := range table {
		if (main || !j.mainOnly) && (media || !j.media) {
			names = append(names, j.name)
		}
	}
	return names
}

func Label(name string) string {
	for _, j := range table {
		if j.name == name {
			return j.label
		}
	}
	return name
}

func OnEveryMachine(name string) bool {
	for _, j := range table {
		if j.name == name {
			return !j.mainOnly
		}
	}
	return false
}

func Unit(installation, job string) string {
	return "mse-" + installation + "-" + job
}
