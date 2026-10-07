package join

import (
	"context"
	"errors"
	"fmt"

	"github.com/pablovarela/media-server-engine/internal/configure"
	"github.com/pablovarela/media-server-engine/internal/create"
	"github.com/pablovarela/media-server-engine/internal/paint"
	"github.com/pablovarela/media-server-engine/internal/report"
)

type Steps interface {
	Clone(ctx context.Context) (create.Undo, error)
	AddKey(ctx context.Context) (create.Undo, error)
	Data(ctx context.Context) (restoring bool, err error)
	Role(ctx context.Context) error
	Apply(ctx context.Context) error
	Summary(ctx context.Context) string
}

type Installation struct{ Name, Logs string }

func Run(ctx context.Context, i Installation, steps Steps, r *report.Reporter, shield func() func()) error {
	var undos []create.Undo
	if err := local(ctx, steps, &undos); err != nil {
		return discard(i, undos, err, shield)
	}
	if restoring, err := steps.Data(ctx); err != nil {
		restore := plainRestoreLeft
		if restoring {
			restore = restoreLeft
		}
		return left(err, remaining{
			stopped: "stopped while restoring " + i.Name + "'s data. Its config and key are on this machine",
			kept:    i.Name + "'s config and key are on this machine; its data isn't restored",
			still:   []string{restore, claimLeft, applyLeft},
		})
	}
	if err := steps.Role(ctx); err != nil {
		return left(err, remaining{
			stopped: "stopped before choosing this machine's role. " + i.Name + "'s config, key and data are on this machine",
			kept:    i.Name + "'s config, key and data are on this machine",
			still:   []string{claimLeft, applyLeft},
		})
	}
	if err := steps.Apply(ctx); err != nil {
		kept := i.Name + "'s config, key and data are on this machine and its role is set"
		return left(err, remaining{stopped: "stopped while applying. " + kept, kept: kept, still: []string{applyLeft}})
	}
	r.Say(paint.Stdout.Success(steps.Summary(ctx)))
	return nil
}

func local(ctx context.Context, steps Steps, undos *[]create.Undo) error {
	for _, step := range []func(context.Context) (create.Undo, error){steps.Clone, steps.AddKey} {
		undo, err := step(ctx)
		if undo != nil {
			*undos = append(*undos, undo)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

const (
	restoreLeft      = "mse restore --apps --overwrite"
	plainRestoreLeft = "mse restore --apps"
	claimLeft        = "mse backup --apps --take-over (to make this machine the main)"
	applyLeft        = "mse apply"
)

type remaining struct {
	stopped, kept string
	still         []string
}

func left(err error, r remaining) error {
	finish := ""
	for _, c := range r.still {
		finish += "\n  " + c
	}
	if stopped(err) {
		return fmt.Errorf("%s. Finish with:%s", r.stopped, finish)
	}
	return fmt.Errorf("%w\n%s. Finish with:%s", err, r.kept, finish)
}

func stopped(err error) bool {
	return errors.Is(err, configure.ErrAborted) || errors.Is(err, context.Canceled)
}

func discard(i Installation, undos []create.Undo, err error, shield func() func()) error {
	release := shield()
	defer release()
	var failed []error
	for n := len(undos) - 1; n >= 0; n-- {
		if undone := undos[n](); undone != nil {
			failed = append(failed, undone)
		}
	}
	kept := "Nothing was kept"
	if len(undos) > 0 {
		kept += " apart from this run's log in " + i.Logs
	}
	again := "run mse setup " + i.Name + " again."
	var changed *create.KeyFileChangedError
	switch {
	case len(failed) == 1 && errors.As(failed[0], &changed):
		return fmt.Errorf("%w\nThe rest was removed, but %s. Then %s", err, changed, again)
	case len(failed) > 0:
		return fmt.Errorf("%w\nRemoving what was written failed: %w\nRemove what is left, then %s", err, errors.Join(failed...), again)
	case stopped(err):
		return fmt.Errorf("stopped before %s joined. %s; %s", i.Name, kept, again)
	}
	return fmt.Errorf("%w\n%s; %s", err, kept, again)
}
