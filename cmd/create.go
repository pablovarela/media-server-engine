package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/configure"
	"github.com/pablovarela/media-server-engine/internal/create"
	"github.com/pablovarela/media-server-engine/internal/gitconfig"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/report"
)

type configRepositories interface {
	Login(ctx context.Context) (string, error)
	RepositoryExists(ctx context.Context, owner, repo string) (bool, error)
	CreatePrivateRepository(ctx context.Context, org, repo string) (string, error)
}

var errCreateNeedsTerminal = errors.New("mse create needs a terminal; run it from an interactive shell (over ssh: ssh -t)")

func newCreateCommand(deps Dependencies) *cobra.Command {
	var owner string
	command := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new installation on this machine: key, config, GitHub repository, backups and the stack",
		Long: "Create a new installation on this machine. It checks the machine, makes a secrets key, writes the config from the template, " +
			"asks for its settings, pushes it to a new private repository media-server-config-<name>, makes this machine the main and applies it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !deps.Terminal() {
				return errCreateNeedsTerminal
			}
			if err := cmd.Flags().Set("installation", ""); err != nil {
				return err
			}
			if err := create.ValidName(args[0]); err != nil {
				return err
			}
			c := deps.creation(cmd, args[0], owner)
			return create.Run(cmd.Context(), create.Installation{Name: c.name, Config: c.config, Data: c.data}, c, report.From(cmd.Context()), shieldSignals)
		},
	}
	command.Flags().StringVar(&owner, "owner", "", "the GitHub organisation that owns the config repository (default: the user gh is logged in as)")
	return command
}

type creation struct {
	d                      Dependencies
	cmd                    *cobra.Command
	name, owner, org, repo string
	config, data, state    string
	recipient              string
	repository             gitconfig.Repository
}

func (d Dependencies) creation(cmd *cobra.Command, name, owner string) *creation {
	bases := installation.BasesFrom(d.Environment, d.Home)
	config := filepath.Join(bases.Config, "mse", name)
	tool := report.From(cmd.Context()).Tool("git")
	return &creation{
		d: d, cmd: cmd, name: name, owner: owner, org: owner, repo: "media-server-config-" + name,
		config: config, data: filepath.Join(bases.Data, "mse", name), state: filepath.Join(bases.State, "mse", name),
		repository: gitconfig.Repository{Runner: d.Run(tool, tool), Dir: config},
	}
}

func (c *creation) CheckMachine(context.Context) error {
	ready, err := c.d.machineReady(c.cmd)
	if err == nil && !ready {
		return errAlreadyReported
	}
	return err
}

func (c *creation) CheckName(ctx context.Context) error {
	for _, folder := range []string{c.config, c.data} {
		if _, err := os.Stat(folder); err == nil {
			return fmt.Errorf("%s already exists, so %s is already an installation here", folder, c.name)
		}
	}
	tool := report.From(ctx).Tool("git")
	known, err := gitconfig.Repository{Runner: c.d.Run(tool, tool), Dir: c.d.Home}.HasIdentity(ctx)
	if err != nil {
		return err
	}
	if !known {
		return errors.New("git has no name or email to commit the config with; set them, then run mse create again:\n" +
			"  git config --global user.name \"Your Name\"\n  git config --global user.email you@example.com")
	}
	login, err := c.d.Repositories.Login(ctx)
	if err != nil {
		return err
	}
	if c.owner == "" || c.owner == login {
		c.owner, c.org = login, ""
	}
	exists, err := c.d.Repositories.RepositoryExists(ctx, c.owner, c.repo)
	if err == nil && exists {
		return fmt.Errorf("%s/%s already exists on GitHub; to add this machine to it, run mse join %s", c.owner, c.repo, c.name)
	}
	return err
}

func (c *creation) AddKey(ctx context.Context) (create.Undo, error) {
	path, err := create.KeyFile(c.d.Environment, installation.BasesFrom(c.d.Environment, c.d.Home).Config)
	if err != nil {
		return nil, err
	}
	key, err := create.NewKey()
	if err != nil {
		return nil, err
	}
	undo, err := create.AppendKey(path, c.name, key, c.d.Now())
	if err != nil {
		return nil, err
	}
	c.recipient = key.Public
	text := "This is " + c.name + "'s secrets key. Save it in your password manager now: with the name " + c.name +
		" it rebuilds the installation on any machine, and without it nobody can read the config's secrets.\n\n" + key.Secret + "\n\nIt is also in " + path + "."
	return undo, c.d.Prompter(ctx).Acknowledge("Secrets key for "+c.name, text, "saved")
}

func (c *creation) WriteConfig(ctx context.Context) (create.Undo, error) {
	template, err := fs.Sub(c.d.Engine, "config-template")
	if err != nil {
		return nil, err
	}
	step := report.From(ctx).Step("Writing the config in " + c.config)
	undo, err := create.WriteConfig(template, c.config, c.name, c.recipient)
	if err != nil {
		return undo, step.FailWithoutTail(err)
	}
	openInstallationLog(ctx, c.state)
	step.Done("done")
	return undo, nil
}

func (c *creation) Configure(ctx context.Context) error {
	current, texts, err := configure.Load(c.config, c.d.Decrypt)
	if err != nil {
		return err
	}
	seeded, err := configure.Rotate(current, configure.Rotatable, c.d.RandomKey)
	if err != nil {
		return err
	}
	outcome, err := configure.Guided(ctx, c.d.Prompter(ctx), current, seeded)
	if err != nil {
		return err
	}
	writer := configure.Writer{Config: c.config, Encrypt: c.d.Encrypt(c.config)}
	_, err = writeChanges(report.From(ctx), writer, configure.Diff(current, outcome.Values), texts)
	return err
}

func (c *creation) Commit(ctx context.Context) error {
	step := report.From(ctx).Step("Committing the config")
	if err := c.repository.Init(ctx); err != nil {
		return step.FailWithoutTail(err)
	}
	sha, err := c.repository.Commit(ctx, "Create "+c.name, []string{"."})
	if err != nil {
		return step.FailWithoutTail(err)
	}
	step.Done(fmt.Sprintf("%s %q", sha, "Create "+c.name))
	return nil
}

func (c *creation) Publish(ctx context.Context) (bool, error) {
	step := report.From(ctx).Step("Creating github.com/" + c.owner + "/" + c.repo + " (private)")
	url, err := c.d.Repositories.CreatePrivateRepository(ctx, c.org, c.repo)
	if err != nil {
		return false, step.FailWithoutTail(err)
	}
	step.Done("done")
	step = report.From(ctx).Step("Pushing the config")
	if err := c.repository.AddRemote(ctx, url); err != nil {
		return true, step.FailWithoutTail(err)
	}
	if err := c.repository.PushNew(ctx); err != nil {
		return true, step.FailWithoutTail(err)
	}
	step.Done("done")
	return true, nil
}

func (c *creation) ClaimMain(context.Context) error {
	if err := c.cmd.Flags().Set("installation", c.name); err != nil {
		return err
	}
	if c.d.ClaimMain != nil {
		return c.d.ClaimMain(c.cmd)
	}
	b, err := c.d.backups(c.cmd, backingUp)
	if err != nil {
		return err
	}
	return b.Claim(c.cmd.Context(), false)
}

func (c *creation) Apply(context.Context) error {
	if c.d.ApplyNew != nil {
		return c.d.ApplyNew(c.cmd)
	}
	return c.d.apply(c.cmd)
}
