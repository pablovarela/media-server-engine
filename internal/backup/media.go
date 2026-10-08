package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pablovarela/media-server-engine/internal/paint"
	"github.com/pablovarela/media-server-engine/internal/restic"
)

const MediaJob = "media-backup"

func MediaDir(data string) string {
	return filepath.Join(data, "data", "media")
}

func mediaLock(data string) (*heldLock, error) {
	return takeLock(filepath.Join(data, ".media-backup.lock"), "media backup")
}

func restorePendingPath(data string) string {
	return filepath.Join(data, ".media-restore-pending")
}

func MarkMediaRestorePending(data string) error {
	return os.WriteFile(restorePendingPath(data), nil, 0o644) //nolint:gosec // an empty marker file
}

func MediaRestorePending(data string) bool {
	_, err := os.Stat(restorePendingPath(data))
	return err == nil
}

func ClearMediaRestorePending(data string) (bool, error) {
	err := os.Remove(restorePendingPath(data))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (b *Backups) BackupMedia(ctx context.Context) (err error) {
	lock, err := mediaLock(b.Installation.Data)
	if err != nil {
		return err
	}
	defer lock.release()
	finishing := context.WithoutCancel(ctx)
	defer func() {
		if err != nil {
			defer b.shielded()()
			b.Pinger.Ping(finishing, MediaJob, "/fail")
		}
	}()
	_, refused, err := b.refuseOnCopy(ctx, MediaJob, "Its media is backed up there; mse backup --apps --take-over makes this machine the main.")
	if err != nil {
		return fmt.Errorf("%w; nothing was backed up", err)
	}
	if refused {
		return nil
	}
	b.Pinger.Ping(ctx, MediaJob, "/start")
	dir, err := b.mediaToBackUp()
	if err != nil {
		return err
	}
	if err := b.snapshotMedia(ctx, lock, dir); err != nil {
		return b.withKeyHint(err)
	}
	if err := b.removeOldMediaSnapshots(ctx, lock); err != nil {
		return err
	}
	if err := b.checkMedia(ctx); err != nil {
		return err
	}
	b.Pinger.Ping(ctx, MediaJob, "")
	b.Report.Say(paint.Stdout.Success("Media backup done."))
	return nil
}

func (b *Backups) mediaToBackUp() (string, error) {
	if MediaRestorePending(b.Installation.Data) {
		return "", fmt.Errorf("%s's media hasn't been restored on this machine yet, so a backup now would replace the media backup with what is here; "+
			"mse restore --media brings it back", b.Installation.Name)
	}
	media := MediaDir(b.Installation.Data)
	dir, err := filepath.EvalSymlinks(media)
	if err != nil {
		return "", fmt.Errorf("cannot read %s (%w); nothing was backed up", media, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("cannot read %s (%w); nothing was backed up", media, err)
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("%s is empty (is its disk mounted?); nothing was backed up", media)
	}
	return dir, nil
}

func (b *Backups) mediaRepositoryReady(ctx context.Context) error {
	exists, err := b.MediaRepository.HasRepository(ctx)
	if err != nil {
		return err
	}
	if !exists {
		step := b.Report.Step("Creating the media backup repository " + b.Media.Repository)
		if err := b.MediaRepository.Init(ctx); err != nil {
			return step.Fail(err)
		}
		step.Done("created")
	}
	return b.MediaRepository.Unlock(ctx)
}

func (b *Backups) snapshotMedia(ctx context.Context, lock *heldLock, dir string) error {
	if err := b.mediaRepositoryReady(ctx); err != nil {
		return err
	}
	step := b.Report.Step("Backing up the media")
	summary, err := b.MediaRepository.Backup(ctx, restic.BackupOptions{
		Host:        b.Installation.Name,
		Tags:        []string{"machine:" + b.MachineID, "machine-name:" + b.ShortHost, "weekly"},
		Dir:         dir,
		Paths:       []string{"."},
		LimitUpload: b.Media.UploadLimit,
		Inherit:     lock.files(),
		Progress:    b.progress(step),
	})
	if err != nil {
		return step.Fail(err)
	}
	step.Done(summary.String())
	return nil
}

func (b *Backups) removeOldMediaSnapshots(ctx context.Context, lock *heldLock) error {
	step := b.Report.Step("Removing old media snapshots")
	// The media folder's resolved path changes with its disk; its snapshots stay one series.
	summary, err := b.MediaRepository.Forget(ctx, b.Installation.Name, restic.Keep{Weekly: b.Media.KeepWeekly, GroupBy: "host"}, lock.files())
	if err != nil {
		return step.Fail(err)
	}
	if len(summary.Removed) == 0 {
		step.Done(summary.String())
		return nil
	}
	if err := b.MediaRepository.Prune(ctx, lock.files()); err != nil {
		return step.Fail(err)
	}
	step.Done(summary.String() + "; pruned")
	return nil
}

func (b *Backups) checkMedia(ctx context.Context) error {
	step := b.Report.Step("Checking " + b.Media.CheckSubset + " of the media backup")
	if err := b.MediaRepository.Check(ctx, b.Media.CheckSubset); err != nil {
		return step.Fail(err)
	}
	step.Done("done")
	return nil
}

func (b *Backups) withKeyHint(err error) error {
	if !strings.Contains(strings.ToLower(err.Error()), "unauthorized") {
		return err
	}
	return fmt.Errorf("%w\nThe storage refused the key for %s. A B2 key limited to a file-name prefix can't reach a repository outside it; "+
		"give the media backup a key that covers the whole bucket", err, b.Media.Repository)
}
