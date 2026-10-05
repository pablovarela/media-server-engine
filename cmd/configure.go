package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/configure"
	"github.com/pablovarela/media-server-engine/internal/gitconfig"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/report"
)

type prompter interface {
	configure.Prompter
}

const notAppliedYet = "\nThe new configuration is NOT applied on this machine yet.\nTo apply it now, run:\n\n    mse apply\n\n" +
	"Otherwise every machine applies it at its next nightly update (05:00).\n"

var errNoTerminal = errors.New("mse configure needs a terminal; run it from an interactive shell (over ssh: ssh -t)")

func newConfigureCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "configure",
		Short: "Change the installation's settings and secrets, then commit and push the config",
		Long: "Change the installation's settings and secrets from a menu, then commit and push the config.\n\n" +
			"Nothing is applied on this machine until mse apply runs, by hand or at the nightly update.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deps.configure(cmd)
		},
	}
}

func (d Dependencies) configure(cmd *cobra.Command) error {
	if !d.Terminal() {
		return errNoTerminal
	}
	i, err := d.anyInstallation(cmd)
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	tool := report.From(ctx).Tool("git")
	repository := gitconfig.Repository{Runner: d.Run(tool, tool), Dir: i.Config}
	if err := checkIdentity(ctx, repository, i.Config); err != nil {
		return err
	}
	if err := d.fetchConfig(cmd, i, "configure"); err != nil {
		return err
	}
	current, texts, err := configure.Load(i.Config, d.Decrypt)
	if err != nil {
		return err
	}
	outcome, err := configure.Session(ctx, d.Prompter(ctx), i.Name, current, d.RandomKey)
	if outcome.NothingChanged {
		report.From(ctx).Say("Nothing changed.")
	}
	if err != nil || !outcome.Save {
		return err
	}
	return d.save(ctx, i, repository, configure.Diff(current, outcome.Values), texts)
}

func checkIdentity(ctx context.Context, repository gitconfig.Repository, config string) error {
	known, err := repository.HasIdentity(ctx)
	if err != nil || known {
		return err
	}
	return fmt.Errorf("git has no name or email to commit the config with; set them, then run mse configure again:\n"+
		"  git -C %s config user.name \"Your Name\"\n  git -C %s config user.email you@example.com", config, config)
}

func (d Dependencies) save(ctx context.Context, i *installation.Installation, repository gitconfig.Repository, changes []configure.Change, texts configure.Texts) error {
	r := report.From(ctx)
	writer := configure.Writer{Config: i.Config, Encrypt: d.Encrypt(i.Config)}
	written, err := writeChanges(r, writer, changes, texts)
	if err != nil {
		return putBack(writer, texts, changes, err)
	}
	step := r.Step("Committing the config")
	message := configure.CommitMessage(i.Name, changes)
	sha, err := repository.Commit(ctx, message, written)
	if err != nil {
		_ = step.FailWithoutTail(err)
		if unstaged := repository.Unstage(context.WithoutCancel(ctx), written); unstaged != nil {
			return fmt.Errorf("%w; unstaging the changes failed too: %w", err, unstaged)
		}
		return putBack(writer, texts, changes, err)
	}
	step.Done(fmt.Sprintf("%s %q", sha, message))
	return push(ctx, r, repository, i.Config)
}

func putBack(writer configure.Writer, texts configure.Texts, changes []configure.Change, err error) error {
	if undone := writer.Undo(texts, changes); undone != nil {
		return fmt.Errorf("%w; putting the config back failed too: %w", err, undone)
	}
	return fmt.Errorf("%w; the config is back as it was", err)
}

func writeChanges(r *report.Reporter, writer configure.Writer, changes []configure.Change, texts configure.Texts) ([]string, error) {
	plain, secretFiles := configure.Files(changes)
	var written []string
	if plain {
		step := r.Step("Writing " + configure.PlainFile)
		if err := writer.WritePlain(texts, changes); err != nil {
			return written, step.FailWithoutTail(err)
		}
		written = append(written, configure.PlainFile)
		step.Done("done")
	}
	if len(secretFiles) == 0 {
		return written, nil
	}
	step := r.Step("Encrypting " + strings.Join(secretFiles, ", "))
	encrypted, err := writer.WriteSecrets(texts, changes)
	written = append(written, encrypted...)
	if err != nil {
		return written, step.FailWithoutTail(err)
	}
	step.Done("done")
	return written, nil
}

func push(ctx context.Context, r *report.Reporter, repository gitconfig.Repository, config string) error {
	remote, err := repository.HasRemote(ctx)
	if err != nil {
		return err
	}
	if !remote {
		r.Step("Pushing").Done("no remote, not pushed")
		r.Say(strings.TrimSuffix(notAppliedYet, "\n"))
		return nil
	}
	url, err := repository.RemoteURL(ctx)
	if err != nil {
		return err
	}
	step := r.Step("Pushing to " + gitconfig.DisplayRemote(url))
	if err := repository.Push(ctx); err != nil {
		_ = step.FailWithoutTail(err)
		r.Say("The change is committed here but not pushed; push it with: git -C " + config + " push")
		r.Say(strings.TrimSuffix(notAppliedYet, "\n"))
		return fmt.Errorf("the config is committed but not pushed: %w", err)
	}
	step.Done("done")
	r.Say(strings.TrimSuffix(notAppliedYet, "\n"))
	return nil
}
