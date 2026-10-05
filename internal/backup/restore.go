package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pablovarela/media-server-engine/internal/restic"
)

func (b *Backups) Restore(ctx context.Context, overwrite bool) error {
	data := b.Installation.Data
	if err := os.MkdirAll(data, 0o755); err != nil { //nolint:gosec // the installation's data directory, read by its containers
		return err
	}
	running, err := b.Stack.AnyRunning(ctx)
	if err != nil {
		return fmt.Errorf("cannot tell whether the stack is running (%w)", err)
	}
	if running {
		return errors.New("the stack is running; stop it with mse stack down first")
	}
	volumes := filepath.Join(data, "volumes")
	held, err := hasAppData(volumes)
	if err != nil {
		return err
	}
	if held {
		if !overwrite {
			return errors.New("volumes/ already holds app data; run with --overwrite to replace it with the latest backup")
		}
		if err := b.moveAside(volumes); err != nil {
			return err
		}
	}
	return b.restoreVolumes(ctx, volumes)
}

func (b *Backups) restoreVolumes(ctx context.Context, volumes string) error {
	step := b.Report.Step("Restoring volumes/ from the latest backup")
	if err := b.Repository.Unlock(ctx); err != nil {
		return step.Fail(err)
	}
	host, err := b.ownHost(ctx)
	if err != nil {
		return step.Fail(fmt.Errorf("cannot read the backup repository's snapshots (%w); nothing was restored", err))
	}
	if err := b.Repository.Restore(ctx, restic.RestoreOptions{Snapshot: "latest:/volumes", Host: host, Target: volumes, Exclude: []string{"configarr"}}); err != nil {
		return step.Fail(err)
	}
	step.Done("restored")
	return nil
}

func hasAppData(volumes string) (bool, error) {
	entries, err := os.ReadDir(volumes)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.Name() != "configarr" {
			return true, nil
		}
	}
	return false, nil
}

func (b *Backups) moveAside(volumes string) error {
	aside := filepath.Join(filepath.Dir(volumes), "volumes.before-restore-"+b.Now().Format("20060102-150405"))
	if err := os.Rename(volumes, aside); err != nil {
		return err
	}
	if err := os.Mkdir(volumes, 0o755); err != nil { //nolint:gosec // the apps' volumes, read by their containers
		return err
	}
	b.Report.Say(fmt.Sprintf("previous volumes/ kept in %s; delete it once the restore looks right", aside))
	return nil
}
