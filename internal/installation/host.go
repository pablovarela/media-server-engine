package installation

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type Host struct {
	GOOS          string
	Hostname      func() (string, error)
	LocalHostName func() (string, error)
}

func SystemHost() Host {
	return Host{
		GOOS:     runtime.GOOS,
		Hostname: os.Hostname,
		LocalHostName: func() (string, error) {
			out, err := exec.CommandContext(context.Background(), "scutil", "--get", "LocalHostName").Output()
			return strings.TrimSpace(string(out)), err
		},
	}
}

func (i *Installation) NetworkName(host Host) (string, error) {
	if name := i.Settings["MEDIA_SERVER_HOST"]; name != "" {
		return name, nil
	}
	if host.GOOS == "darwin" {
		name, err := host.LocalHostName()
		return name + ".local", err
	}
	hostname, err := host.Hostname()
	if err != nil {
		return "", err
	}
	short, _, _ := strings.Cut(hostname, ".")
	return short + ".local", nil
}
