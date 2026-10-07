package timers

import (
	"embed"
	"strings"
)

//go:embed units
var units embed.FS

type Values struct {
	Installation string
	Executable   string
	Environment  []string
}

type Job struct {
	Name string
}

var (
	Update  = Job{Name: "update"}
	Cleanup = Job{Name: "download-cleanup"}
	Backup  = Job{Name: "backup"}
	Verify  = Job{Name: "verify"}
)

func (j Job) Unit(installation string) string {
	return "mse-" + installation + "-" + j.Name
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

func Plan(role Role) (install, remove []Job) {
	switch role {
	case Main:
		return []Job{Update, Cleanup, Backup, Verify}, nil
	case Secondary:
		return []Job{Update, Cleanup}, []Job{Backup, Verify}
	}
	return []Job{Update, Cleanup}, nil
}

func Render(job Job, suffix string, v Values) (string, error) {
	template, err := units.ReadFile("units/" + job.Name + suffix)
	if err != nil {
		return "", err
	}
	env := environment(v)
	if env == "" {
		return strings.NewReplacer("@NAME@", v.Installation, "@ENVIRONMENT@\n", "", "@MSE@", v.Executable).Replace(string(template)), nil
	}
	return strings.NewReplacer("@NAME@", v.Installation, "@ENVIRONMENT@", env, "@MSE@", v.Executable).Replace(string(template)), nil
}

func environment(v Values) string {
	var lines []string
	quoting := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%")
	for _, variable := range v.Environment {
		lines = append(lines, `Environment="`+quoting.Replace(variable)+`"`)
	}
	return strings.Join(lines, "\n")
}
