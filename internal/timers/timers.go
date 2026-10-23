package timers

import (
	"embed"
	"strings"

	"github.com/pablovarela/media-server-engine/internal/jobs"
)

//go:embed units
var units embed.FS

type Values struct {
	Installation  string
	Executable    string
	Environment   []string
	MediaSchedule string
}

type Job struct {
	Name string
}

var (
	Update         = Job{Name: jobs.Update}
	CleanDownloads = Job{Name: jobs.CleanDownloads}
	BackupApps     = Job{Name: jobs.BackupApps}
	CheckBackup    = Job{Name: jobs.CheckBackup}
	BackupMedia    = Job{Name: jobs.BackupMedia}
)

func (j Job) Unit(installation string) string {
	return jobs.Unit(installation, j.Name)
}

func (j Job) Files(installation string) []string {
	unit := j.Unit(installation)
	return []string{unit + ".service", unit + ".timer"}
}

type Role int

const (
	Secondary Role = iota
	Main
	Unknown
)

func Plan(role Role, media bool) (install, keep []Job) {
	for _, name := range jobs.Planned(role == Main, media) {
		install = append(install, Job{Name: name})
	}
	if role == Unknown {
		for _, name := range jobs.Planned(true, true) {
			if !jobs.OnEveryMachine(name) {
				keep = append(keep, Job{Name: name})
			}
		}
	}
	return install, keep
}

func Render(job Job, suffix string, v Values) (string, error) {
	template, err := units.ReadFile("units/" + job.Name + suffix)
	if err != nil {
		return "", err
	}
	env := environment(v)
	if env == "" {
		return strings.NewReplacer("@NAME@", v.Installation, "@ENVIRONMENT@\n", "", "@MSE@", v.Executable, "@SCHEDULE@", v.MediaSchedule).Replace(string(template)), nil
	}
	return strings.NewReplacer("@NAME@", v.Installation, "@ENVIRONMENT@", env, "@MSE@", v.Executable, "@SCHEDULE@", v.MediaSchedule).Replace(string(template)), nil
}

func environment(v Values) string {
	var lines []string
	quoting := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%")
	for _, variable := range v.Environment {
		lines = append(lines, `Environment="`+quoting.Replace(variable)+`"`)
	}
	return strings.Join(lines, "\n")
}
