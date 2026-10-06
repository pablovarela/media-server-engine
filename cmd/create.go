package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/configure"
	"github.com/pablovarela/media-server-engine/internal/create"
	"github.com/pablovarela/media-server-engine/internal/gitconfig"
	"github.com/pablovarela/media-server-engine/internal/github"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/report"
)

type configRepositories interface {
	Login(ctx context.Context) (string, error)
	RepositoryExists(ctx context.Context, owner, repo string) (bool, error)
	CreatePrivateRepository(ctx context.Context, org, repo string) (string, error)
}

const homepagePortKey = "HOMEPAGE_PORT"

var errCreateNeedsTerminal = errors.New("mse create needs a terminal; run it from an interactive shell (over ssh: ssh -t)")

var errCreateTakesTheName = errors.New("mse create takes the installation's name as its argument; it has no --installation")

func newCreateCommand(deps Dependencies) *cobra.Command {
	var owner, homepagePort string
	command := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new installation on this machine: key, config, GitHub repository, backups and the stack",
		Long: "Create a new installation on this machine. It checks the machine, makes a secrets key, writes the config from the template, " +
			"asks for its settings, pushes it to a new private repository media-server-config-<name>, makes this machine the main and applies it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := deps.canCreate(cmd, args[0], homepagePort); err != nil {
				return err
			}
			c := deps.creation(cmd, args[0], owner)
			c.homepagePort = homepagePort
			return create.Run(cmd.Context(), create.Installation{Name: c.name, Config: c.config}, c, report.From(cmd.Context()), shieldSignals)
		},
	}
	command.Flags().StringVar(&owner, "owner", "", "the GitHub organisation that owns the config repository (default: the user gh is logged in as)")
	command.Flags().StringVar(&homepagePort, "homepage-port", "", "the landing page's port, when something else on this machine uses port 80")
	return command
}

func (d Dependencies) canCreate(cmd *cobra.Command, name, homepagePort string) error {
	switch {
	case !d.Terminal():
		return errCreateNeedsTerminal
	case cmd.Flags().Changed("installation"):
		return errCreateTakesTheName
	}
	if err := create.ValidName(name); err != nil {
		return err
	}
	if err := validHomepagePort(homepagePort); err != nil {
		return err
	}
	return d.noOtherInstallation(name)
}

func validHomepagePort(port string) error {
	for _, section := range configure.Sections() {
		for _, field := range section.Fields {
			if field.Key != homepagePortKey {
				continue
			}
			if err := field.Validate(port); err != nil {
				return fmt.Errorf("--homepage-port: %w", err)
			}
		}
	}
	return nil
}

func (d Dependencies) noOtherInstallation(name string) error {
	names, err := installation.Names(installation.BasesFrom(d.Environment, d.Home))
	if err != nil {
		return err
	}
	others := slices.DeleteFunc(names, func(n string) bool { return n == name })
	if len(others) > 0 {
		return fmt.Errorf("this machine already runs the installation %s, and a machine runs one installation's stack", strings.Join(others, ", "))
	}
	return nil
}

type creation struct {
	d                       Dependencies
	cmd                     *cobra.Command
	name, owner, org, repo  string
	config, data, state     string
	recipient, homepagePort string
	remote                  string
	repository              gitconfig.Repository
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

func (c *creation) CheckMachine(ctx context.Context) error {
	ports, err := c.d.stackPortsCheck(nil, c.homepagePort)
	if err != nil {
		return err
	}
	machine, err := c.d.checkedMachine(c.cmd, ports)
	switch {
	case err != nil:
		return err
	case machine.Ready():
		return nil
	case machine.PortsTaken() && c.homepagePort == "":
		report.From(ctx).Say("If port 80 is in use by something you keep, give the landing page another port: mse create " + c.name + " --homepage-port <port>")
	}
	return errAlreadyReported
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
	if c.owner == "" || strings.EqualFold(c.owner, login) {
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
	_, _ = fmt.Fprintf(report.From(ctx).Data(), "\nThis is %s's secrets key. Save it in your password manager now: with the name %s it rebuilds the installation on any machine,\n"+
		"and without it nobody can read the config's secrets. It is also in %s.\n\n    %s\n\n", c.name, c.name, path, key.Secret)
	return undo, c.d.Prompter(ctx).Acknowledge("Saved "+c.name+"'s secrets key?", "It is printed above. Once it is in your password manager, type saved.", "saved")
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
	if c.homepagePort != "" {
		seeded = seeded.With(configure.PlainFile, homepagePortKey, c.homepagePort)
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
		_ = step.FailWithoutTail(err)
		return c.afterFailedCreate(ctx, err)
	}
	step.Done("done")
	c.remote = gitconfig.DisplayRemote(url)
	step = report.From(ctx).Step("Pushing the config")
	if err := c.repository.AddRemote(ctx, url); err != nil {
		return true, step.FailWithoutTail(c.addRemoteBy(err, url))
	}
	if err := c.repository.PushNew(ctx); err != nil {
		return true, step.FailWithoutTail(err)
	}
	step.Done("done")
	return true, nil
}

func (c *creation) afterFailedCreate(ctx context.Context, err error) (bool, error) {
	if errors.Is(err, github.ErrRepositoryTaken) {
		return false, create.NameTaken(err)
	}
	exists, lookup := c.d.Repositories.RepositoryExists(context.WithoutCancel(ctx), c.owner, c.repo)
	if lookup != nil || !exists {
		return false, err
	}
	return true, c.addRemoteBy(err, "https://github.com/"+c.owner+"/"+c.repo+".git")
}

func (c *creation) addRemoteBy(err error, url string) error {
	return fmt.Errorf("%w\nAdd its remote: git -C %s remote add origin %s", err, c.config, url)
}

func (c *creation) Summary(context.Context) string {
	ready := c.name + " is ready"
	i, err := c.d.installation(c.cmd)
	if err != nil {
		return ready + ". mse urls lists every app."
	}
	host, err := i.NetworkName(c.d.Host)
	if err != nil {
		return ready + ". mse urls lists every app."
	}
	page := "http://" + host
	if port := i.HomepagePort(); port != "80" {
		page += ":" + port
	}
	return fmt.Sprintf("%s: its config is in %s (%s), its data in %s and its landing page at %s. mse urls lists every app.", ready, c.config, c.remote, c.data, page)
}

func (c *creation) ClaimMain(context.Context) error {
	if err := c.cmd.Flags().Set("installation", c.name); err != nil {
		return err
	}
	b, err := c.d.backups(c.cmd, backingUp)
	if err != nil {
		return err
	}
	return b.Claim(c.cmd.Context(), false)
}

func (c *creation) Apply(context.Context) error {
	return c.d.apply(c.cmd)
}
