package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/create"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/join"
	"github.com/pablovarela/media-server-engine/internal/report"
)

func newSetupCommand(deps Dependencies) *cobra.Command {
	var owner, homepagePort string
	var overwrite bool
	command := &cobra.Command{
		Use:   "setup <name>",
		Short: "Set up this machine for an installation: create it when its config repository doesn't exist, or rebuild it from its config and backups",
		Long: "Set up this machine for the installation <name>. When the config repository media-server-config-<name> doesn't exist, it asks, " +
			"then creates the installation: a secrets key, the config from the template, its settings, a new private repository, the backups and the stack. " +
			"When it exists, it rebuilds the installation here: it clones the config, takes the secrets key, restores the latest backup, asks whether this machine becomes the main and applies it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := deps.canInstall(cmd, "setup", name, func() error { return validHomepagePort(homepagePort) }, func() error { return deps.notSetUp(name) }); err != nil {
				return err
			}
			login, err := deps.Repositories.Login(cmd.Context())
			if err != nil {
				return err
			}
			if owner == "" || strings.EqualFold(owner, login) {
				owner = login
			}
			exists, err := deps.Repositories.RepositoryExists(cmd.Context(), owner, "media-server-config-"+name)
			if err != nil {
				return err
			}
			if exists {
				return deps.rebuild(cmd, name, owner, homepagePort, overwrite)
			}
			return deps.createNew(cmd, name, owner, login, homepagePort)
		},
	}
	command.Flags().StringVar(&owner, "owner", "", "the GitHub organisation that owns the config repository (default: the user gh is logged in as)")
	command.Flags().StringVar(&homepagePort, "homepage-port", "", "for a new installation: the landing page's port, when something else on this machine uses port 80")
	command.Flags().BoolVar(&overwrite, "overwrite", false, "when rebuilding: move the app data already here aside and restore the latest backup")
	return command
}

func (d Dependencies) notSetUp(name string) error {
	config := filepath.Join(installation.BasesFrom(d.Environment, d.Home).Config, "mse", name)
	if _, err := os.Stat(config); err == nil {
		return fmt.Errorf("%s is already set up on this machine; mse status shows its state", name)
	}
	return nil
}

func (d Dependencies) createNew(cmd *cobra.Command, name, owner, login, homepagePort string) error {
	c := d.creation(cmd, name, owner)
	if owner == login {
		c.org = ""
	}
	c.homepagePort = homepagePort
	return create.Run(cmd.Context(), create.Installation{Name: name, Config: c.config, Logs: filepath.Join(c.state, "logs")}, c, report.From(cmd.Context()), shieldSignals)
}

func (d Dependencies) rebuild(cmd *cobra.Command, name, owner, homepagePort string, overwrite bool) error {
	if homepagePort != "" {
		return fmt.Errorf("--homepage-port only applies to a new installation; %s's port comes from its config", name)
	}
	report.From(cmd.Context()).Say(fmt.Sprintf("Rebuilding %s from %s/media-server-config-%s.", name, owner, name))
	j := d.joining(cmd, name, owner)
	j.restoreOver = overwrite
	return join.Run(cmd.Context(), join.Installation{Name: name, Logs: filepath.Join(j.state, "logs")}, j, report.From(cmd.Context()), shieldSignals)
}
