package timers

import (
	"embed"
	"strings"
)

//go:embed units
var units embed.FS

type Values struct {
	User         string
	Group        string
	Installation string
	Executable   string
}

type Unit struct {
	Name string
}

var (
	Update  = Unit{Name: "media-update"}
	Cleanup = Unit{Name: "media-download-cleanup"}
	Backup  = Unit{Name: "media-backup"}
	Verify  = Unit{Name: "media-verify"}
)

func (u Unit) Files() []string {
	return []string{u.Name + ".service", u.Name + ".timer"}
}

func Render(file string, v Values) (string, error) {
	template, err := units.ReadFile("units/" + file)
	if err != nil {
		return "", err
	}
	return strings.NewReplacer("@USER@", v.User, "@GROUP@", v.Group, "@INSTALLATION@", v.Installation, "@MSE@", v.Executable).Replace(string(template)), nil
}

func Plan(main bool) (install, remove []Unit) {
	if main {
		return []Unit{Update, Cleanup, Backup, Verify}, nil
	}
	return []Unit{Update, Cleanup}, []Unit{Backup, Verify}
}
