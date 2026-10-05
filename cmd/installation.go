package cmd

import (
	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/installation"
)

func (d Dependencies) installation(cmd *cobra.Command) (*installation.Installation, error) {
	requested, _ := cmd.Flags().GetString("installation")
	if requested == "" {
		requested = d.Environment("MSE_INSTALLATION")
	}
	loaded, err := installation.Load(installation.BasesFrom(d.Environment, d.Home), requested, func(name string) string {
		if name == "HOME" {
			return d.Home
		}
		return d.Environment(name)
	})
	if err != nil {
		return nil, err
	}
	return loaded, loaded.CheckSchema(d.Build.Major())
}
