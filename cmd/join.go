package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/backup"
	"github.com/pablovarela/media-server-engine/internal/create"
	"github.com/pablovarela/media-server-engine/internal/gitconfig"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/join"
	"github.com/pablovarela/media-server-engine/internal/report"
)

type joining struct {
	d                           Dependencies
	cmd                         *cobra.Command
	name, owner, repo           string
	config, data, state         string
	restoreOver, restored, kept bool
	role                        join.Role
	current                     *backup.Main
	backups                     *backup.Backups
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

func (j *joining) CheckName(context.Context) error {
	return nil
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
		if errors.Is(err, installation.ErrNewerSchema) {
			err = fmt.Errorf("%w; mse update --force installs the release this config needs", err)
		}
		return j.removeClone(), err
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
	if _, err := join.Recipients(j.config); err != nil {
		return nil, err
	}
	path, err := create.KeyFile(j.d.Environment, installation.BasesFrom(j.d.Environment, j.d.Home).Config)
	if err != nil {
		return nil, err
	}
	matched := map[string]create.Key{}
	pasted, err := j.d.Prompter(ctx).Secret(j.name+"'s secrets key", "Paste it from your password manager (the line starting AGE-SECRET-KEY-1).", func(pasted string) error {
		key, err := join.MatchKey(pasted, j.config)
		if err == nil {
			matched[pasted] = key
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	key, ok := matched[pasted]
	if !ok {
		return nil, errors.New("the prompt returned a key it didn't check")
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
	for _, file := range files {
		if _, err := j.d.Decrypt(file); err != nil {
			return false
		}
	}
	return len(files) > 0
}

func (j *joining) Data(ctx context.Context) (bool, error) {
	b, err := j.d.backups(j.cmd, backingUp)
	if err != nil {
		return false, err
	}
	j.backups = b
	if j.current, err = b.CurrentMain(ctx); err != nil {
		return false, err
	}
	held, err := b.HasAppData()
	if err != nil {
		return false, err
	}
	plan := join.PlanData(j.data, j.current, held, j.restoreOver)
	if plan.Say != "" {
		report.From(ctx).Say(plan.Say)
	}
	j.kept = held && !plan.Restore
	if !plan.Restore {
		return false, nil
	}
	j.restored = true
	return true, b.Restore(ctx, plan.Overwrite)
}

func (j *joining) Role(ctx context.Context) error {
	decision := join.Decide(j.name, j.current, j.d.Now(), j.kept)
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
			summary += fmt.Sprintf(": its config is in %s, its data in %s and its landing page at %s", j.config, j.data, homepageAddress(i, host))
		}
	}
	summary += "."
	if j.role == join.Primary {
		summary += " This machine is its main and backs it up."
	} else if j.current != nil {
		summary += " " + j.current.Machine + " stays its main; mse backup --take-over takes over later."
	}
	if j.restored {
		summary += " The backup holds app state, not media: copy the media over, or rescan each app once it's here."
	}
	return summary
}
