package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/pablovarela/media-server-engine/internal/restic"
)

func (b *Backups) Restore(ctx context.Context, overwrite bool) error {
	data := b.Installation.Data
	if err := os.MkdirAll(data, 0o755); err != nil { //nolint:gosec // the installation's data directory, read by its containers
		return err
	}
	if err := b.refuseWhileRunning(ctx); err != nil {
		return err
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

func (b *Backups) refuseWhileRunning(ctx context.Context) error {
	running, err := b.Stack.AnyRunning(ctx)
	if err != nil {
		return fmt.Errorf("cannot tell whether the stack is running (%w)", err)
	}
	if running {
		return errors.New("the stack is running; stop it with mse stack down first")
	}
	return nil
}

func (b *Backups) RestoreMedia(ctx context.Context) error {
	if err := b.refuseWhileRunning(ctx); err != nil {
		return err
	}
	lock, err := mediaLock(b.Installation.Data)
	if err != nil {
		return err
	}
	defer lock.release()
	snapshots, err := b.MediaRepository.Snapshots(ctx, b.Installation.Name)
	if err != nil {
		return fmt.Errorf("cannot read the media backup repository's snapshots (%w); nothing was restored", err)
	}
	if len(snapshots) == 0 {
		return fmt.Errorf("%s has no media backup yet; start without it with mse apply", b.Installation.Name)
	}
	if err := os.MkdirAll(MediaDir(b.Installation.Data), 0o755); err != nil { //nolint:gosec // the media folder, read by the containers
		return err
	}
	target, err := filepath.EvalSymlinks(MediaDir(b.Installation.Data))
	if err != nil {
		return err
	}
	step := b.Report.Step(fmt.Sprintf("Restoring the media from the latest media backup into %s (%sfiles already there that match are kept)", target, b.freeIn(target)))
	if err := b.MediaRepository.Unlock(ctx); err != nil {
		return step.Fail(err)
	}
	if err := b.MediaRepository.Restore(ctx, restic.RestoreOptions{Snapshot: "latest", Host: b.Installation.Name, Target: target, Overwrite: "if-changed", Progress: b.progress(step)}); err != nil {
		return step.Fail(err)
	}
	if _, err := ClearMediaRestorePending(b.Installation.Data); err != nil {
		return step.Fail(err)
	}
	step.Done("restored")
	return nil
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

func (b *Backups) HasAppData() (bool, error) {
	return hasAppData(filepath.Join(b.Installation.Data, "volumes"))
}

var engineState = []string{"configarr", ".wiring"}

func hasAppData(volumes string) (bool, error) {
	entries, err := os.ReadDir(volumes)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if !slices.Contains(engineState, entry.Name()) {
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

func (b *Backups) freeIn(dir string) string {
	if b.FreeSpace == nil {
		return ""
	}
	free, err := b.FreeSpace(dir)
	if err != nil {
		return ""
	}
	return restic.Size(int64(free)) + " free; " //nolint:gosec // free space on a disk fits in an int64
}

func FreeSpace(dir string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil //nolint:gosec // a block size is never negative
}
