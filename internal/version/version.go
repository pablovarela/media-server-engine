package version

import (
	"fmt"
	"runtime/debug"
)

var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

type Build struct {
	Version string
	Commit  string
	Date    string
}

func Current() Build {
	return resolve(Build{Version: Version, Commit: Commit, Date: Date}, debug.ReadBuildInfo)
}

func resolve(stamped Build, readBuildInfo func() (*debug.BuildInfo, bool)) Build {
	if stamped.Commit != "" {
		return stamped
	}
	info, ok := readBuildInfo()
	if !ok {
		return stamped
	}
	stamped.Commit = commitOf(info)
	return stamped
}

func commitOf(info *debug.BuildInfo) string {
	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	commit := settings["vcs.revision"]
	if len(commit) > 7 {
		commit = commit[:7]
	}
	if commit != "" && settings["vcs.modified"] == "true" {
		commit += "-dirty"
	}
	return commit
}

func (b Build) String() string {
	switch {
	case b.Commit != "" && b.Date != "":
		return fmt.Sprintf("mse %s (commit %s, built %s)", b.Version, b.Commit, b.Date)
	case b.Commit != "":
		return fmt.Sprintf("mse %s (commit %s)", b.Version, b.Commit)
	default:
		return "mse " + b.Version
	}
}
