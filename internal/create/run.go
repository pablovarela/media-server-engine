package create

import (
	"context"
	"errors"
	"fmt"

	"github.com/pablovarela/media-server-engine/internal/configure"
	"github.com/pablovarela/media-server-engine/internal/paint"
	"github.com/pablovarela/media-server-engine/internal/report"
)

type Steps interface {
	CheckMachine(ctx context.Context) error
	CheckName(ctx context.Context) error
	AddKey(ctx context.Context) (Undo, error)
	WriteConfig(ctx context.Context) (Undo, error)
	Configure(ctx context.Context) error
	Commit(ctx context.Context) error
	Publish(ctx context.Context) (created bool, err error)
	ClaimMain(ctx context.Context) error
	Apply(ctx context.Context) error
}

type Installation struct{ Name, Config, Data string }

func Run(ctx context.Context, i Installation, steps Steps, r *report.Reporter, shield func() func()) error {
	if err := steps.CheckMachine(ctx); err != nil {
		return err
	}
	if err := steps.CheckName(ctx); err != nil {
		return err
	}
	var undos []Undo
	if err := local(ctx, steps, &undos); err != nil {
		return discard(i, undos, err, shield)
	}
	created, err := steps.Publish(ctx)
	switch {
	case err != nil && !created:
		return discard(i, undos, err, shield)
	case err != nil:
		return fmt.Errorf("%w\n%s's repository exists and its config is committed in %s, but not pushed. Finish with:\n  git -C %s push --set-upstream origin main\n  mse claim-backup-main\n  mse apply", err, i.Name, i.Config, i.Config)
	}
	if err := steps.ClaimMain(ctx); err != nil {
		return fmt.Errorf("%w\n%s is created and its config pushed. Finish with:\n  mse claim-backup-main\n  mse apply", err, i.Name)
	}
	if err := steps.Apply(ctx); err != nil {
		return fmt.Errorf("%w\n%s is created, its config pushed and this machine is its main. Finish with:\n  mse apply", err, i.Name)
	}
	r.Say(paint.Stdout.Success(fmt.Sprintf("%s is ready. Its config is in %s and its data in %s; mse urls lists the apps' addresses.", i.Name, i.Config, i.Data)))
	return nil
}

func local(ctx context.Context, steps Steps, undos *[]Undo) error {
	for _, step := range []func(context.Context) (Undo, error){steps.AddKey, steps.WriteConfig} {
		undo, err := step(ctx)
		if undo != nil {
			*undos = append(*undos, undo)
		}
		if err != nil {
			return err
		}
	}
	if err := steps.Configure(ctx); err != nil {
		return err
	}
	return steps.Commit(ctx)
}

func discard(i Installation, undos []Undo, err error, shield func() func()) error {
	release := shield()
	defer release()
	var failed []error
	for n := len(undos) - 1; n >= 0; n-- {
		if undone := undos[n](); undone != nil {
			failed = append(failed, undone)
		}
	}
	again := "run mse create " + i.Name + " again."
	var changed *KeyFileChangedError
	switch {
	case len(failed) == 1 && errors.As(failed[0], &changed):
		return fmt.Errorf("%w\nThe rest was removed, but %s. Then %s", err, changed, again)
	case len(failed) > 0:
		return fmt.Errorf("%w\nRemoving what was written failed: %w\nRemove what is left, then %s", err, errors.Join(failed...), again)
	case errors.Is(err, configure.ErrAborted):
		return fmt.Errorf("stopped before %s was created. Nothing was kept; %s", i.Name, again)
	}
	return fmt.Errorf("%w\nNothing was kept; %s", err, again)
}
