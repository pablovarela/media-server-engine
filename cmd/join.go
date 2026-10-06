package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/backup"
	"github.com/pablovarela/media-server-engine/internal/create"
	"github.com/pablovarela/media-server-engine/internal/gitconfig"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/join"
	"github.com/pablovarela/media-server-engine/internal/report"
)

var errBothRoles = errors.New("--main and --secondary can't both be given")

func newJoinCommand(deps Dependencies) *cobra.Command {
	var owner string
	var flags join.Flags
	var restoreOver bool
	command := &cobra.Command{
		Use:   "join <name>",
		Short: "Add this machine to an installation whose config is on GitHub, or rebuild one: config, key, data, role and the stack",
		Long: "Add this machine to an installation whose config is on GitHub, or rebuild one after losing its machine. It checks the machine, " +
			"clones the config, takes the secrets key, restores the latest backup, asks whether this machine becomes the main and applies it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bothRoles := func() error {
				if flags.Main && flags.Secondary {
					return errBothRoles
				}
				return nil
			}
			if err := deps.canInstall(cmd, "join", args[0], bothRoles); err != nil {
				return err
			}
			j := deps.joining(cmd, args[0], owner)
			j.flags, j.restoreOver = flags, restoreOver
			return join.Run(cmd.Context(), join.Installation{Name: j.name, Logs: filepath.Join(j.state, "logs")}, j, report.From(cmd.Context()), shieldSignals)
		},
	}
	command.Flags().StringVar(&owner, "owner", "", "the GitHub organisation that owns the config repository (default: the user gh is logged in as)")
	command.Flags().BoolVar(&flags.Main, "main", false, "make this machine the main without asking")
	command.Flags().BoolVar(&flags.Secondary, "secondary", false, "keep this machine a secondary without asking")
	command.Flags().BoolVar(&restoreOver, "restore-over", false, "move the app data already here aside and restore the latest backup")
	return command
}

type joining struct {
	d                     Dependencies
	cmd                   *cobra.Command
	name, owner, repo     string
	config, data, state   string
	flags                 join.Flags
	restoreOver, restored bool
	role                  join.Role
	current               *backup.Main
	backups               *backup.Backups
}

func (d Dependencies) joining(cmd *cobra.Command, name, owner string) *joining {
	bases := installation.BasesFrom(d.Environment, d.Home)
	return &joining{
		d: d, cmd: cmd, name: name, owner: owner, repo: "media-server-config-" + name,
		config: filepath.Join(bases.Config, "mse", name), data: filepath.Join(bases.Data, "mse", name), state: filepath.Join(bases.State, "mse", name),
	}
}

func (j *joining) CheckMachine(context.Context) error {
	return j.d.machineReadyFor(j.cmd, "", "If something else must keep port 80, change "+j.name+"'s homepage port with mse configure on a machine that has it.")
}

func (j *joining) CheckName(ctx context.Context) error {
	if _, err := os.Stat(j.config); err == nil {
		return fmt.Errorf("this machine already has %s in %s; mse update brings it up to date", j.name, j.config)
	}
	login, err := j.d.Repositories.Login(ctx)
	if err != nil {
		return err
	}
	if j.owner == "" || strings.EqualFold(j.owner, login) {
		j.owner = login
	}
	exists, err := j.d.Repositories.RepositoryExists(ctx, j.owner, j.repo)
	if err == nil && !exists {
		return fmt.Errorf("%s/%s isn't on GitHub; to make a new installation, run mse create %s", j.owner, j.repo, j.name)
	}
	return err
}

func (j *joining) Clone(ctx context.Context) (create.Undo, error) {
	url := "https://github.com/" + j.owner + "/" + j.repo + ".git"
	step := report.From(ctx).Step("Cloning " + gitconfig.DisplayRemote(url))
	if err := os.MkdirAll(filepath.Dir(j.config), 0o755); err != nil { //nolint:gosec // the XDG config folder
		return nil, step.FailWithoutTail(err)
	}
	tool := report.From(ctx).Tool("git")
	if err := gitconfig.Clone(ctx, j.d.Run(tool, tool), url, j.config); err != nil {
		failed := fmt.Errorf("%w; check gh auth status and that this account can read %s/%s", err, j.owner, j.repo)
		if removed := j.removeClone()(); removed != nil {
			failed = fmt.Errorf("%w; removing %s failed too: %w", failed, j.config, removed)
		}
		return nil, step.FailWithoutTail(failed)
	}
	step.Done("done")
	openInstallationLog(ctx, j.state)
	if err := j.cmd.Flags().Set("installation", j.name); err != nil {
		return j.removeClone(), err
	}
	if _, err := j.d.installation(j.cmd); err != nil {
		return j.removeClone(), fmt.Errorf("%w; mse update --force installs the release this config needs", err)
	}
	return j.removeClone(), nil
}

func (j *joining) removeClone() create.Undo {
	return func() error { return os.RemoveAll(j.config) }
}

func (j *joining) AddKey(ctx context.Context) (create.Undo, error) {
	if j.keyWorks() {
		report.From(ctx).Step("Secrets key").Done("already on this machine")
		return nil, nil
	}
	path, err := create.KeyFile(j.d.Environment, installation.BasesFrom(j.d.Environment, j.d.Home).Config)
	if err != nil {
		return nil, err
	}
	pasted, err := j.d.Prompter(ctx).Secret(j.name+"'s secrets key", "Paste it from your password manager (the line starting AGE-SECRET-KEY-1).", func(pasted string) error {
		_, err := join.MatchKey(pasted, j.config)
		return err
	})
	if err != nil {
		return nil, err
	}
	key, err := join.MatchKey(pasted, j.config)
	if err != nil {
		return nil, err
	}
	undo, err := create.AppendKey(path, j.name, "added", key, j.d.Now())
	if err != nil {
		return nil, err
	}
	if !j.keyWorks() {
		return undo, errors.New("the key matches the config but doesn't decrypt its secrets")
	}
	report.From(ctx).Step("Secrets key").Done("added to " + path)
	return undo, nil
}

func (j *joining) keyWorks() bool {
	files, _ := filepath.Glob(filepath.Join(j.config, "secrets", "*.sops.env"))
	if len(files) == 0 {
		return false
	}
	_, err := j.d.Decrypt(files[0])
	return err == nil
}

func (j *joining) Data(ctx context.Context) error {
	b, err := j.d.backups(j.cmd, backingUp)
	if err != nil {
		return err
	}
	j.backups = b
	if j.current, err = b.CurrentMain(ctx); err != nil {
		return err
	}
	held, err := b.HasAppData()
	if err != nil {
		return err
	}
	plan := join.PlanData(j.data, j.current, held, j.restoreOver)
	if plan.Say != "" {
		report.From(ctx).Say(plan.Say)
	}
	if !plan.Restore {
		return nil
	}
	j.restored = true
	return b.Restore(ctx, plan.Overwrite)
}

func (j *joining) Role(ctx context.Context) error {
	decision := join.Decide(j.name, j.current, j.d.Now(), j.flags)
	if decision.Say != "" {
		report.From(ctx).Say(decision.Say)
	}
	j.role = decision.Role
	if decision.Ask {
		yes, err := j.d.Prompter(ctx).Ask(decision.Question, decision.Default == join.Primary)
		if err != nil {
			return err
		}
		j.role = join.Secondary
		if yes {
			j.role = join.Primary
		}
	}
	if j.role == join.Secondary || j.current != nil && j.current.ThisMachine {
		return nil
	}
	return j.backups.Claim(ctx, true)
}

func (j *joining) Apply(context.Context) error {
	return j.d.apply(j.cmd)
}

func (j *joining) Summary(context.Context) string {
	summary := j.name + " is running on this machine"
	if i, err := j.d.installation(j.cmd); err == nil {
		if host, err := i.NetworkName(j.d.Host); err == nil {
			page := "http://" + host
			if port := i.HomepagePort(); port != "80" {
				page += ":" + port
			}
			summary += fmt.Sprintf(": its config is in %s, its data in %s and its landing page at %s", j.config, j.data, page)
		}
	}
	summary += "."
	if j.role == join.Primary {
		summary += " This machine is its main and backs it up."
	} else if j.current != nil {
		summary += " " + j.current.Machine + " stays its main; mse claim-backup-main takes over later."
	}
	if j.restored {
		summary += " The backup holds app state, not media: copy the media over, or rescan each app once it's here."
	}
	return summary
}
