package cmd

import (
	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"

	"github.com/pablovarela/media-server-engine/internal/installation"
)

func (d Dependencies) installation(cmd *cobra.Command) (*installation.Installation, error) {
	loaded, err := d.anyInstallation(cmd)
	if err != nil || !semver.IsValid(d.Build.Version) {
		return loaded, err
	}
	return loaded, loaded.CheckSchema(d.Build.Major())
}

func (d Dependencies) anyInstallation(cmd *cobra.Command) (*installation.Installation, error) {
	loaded, err := installation.Load(installation.BasesFrom(d.Environment, d.Home), func(name string) string {
		if name == "HOME" {
			return d.Home
		}
		return d.Environment(name)
	})
	if err != nil {
		return nil, err
	}
	openInstallationLog(cmd.Context(), loaded.State)
	return loaded, nil
}
