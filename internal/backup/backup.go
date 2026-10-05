package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pablovarela/media-server-engine/internal/paint"
	"github.com/pablovarela/media-server-engine/internal/restic"
)

func (b *Backups) Backup(ctx context.Context) error {
	return b.backup(ctx, false)
}

func (b *Backups) Claim(ctx context.Context, yes bool) error {
	exists, err := b.Repository.HasRepository(ctx)
	if err != nil {
		return err
	}
	if !exists {
		b.say("Creating the backup repository " + b.RepositoryLocation)
		if err := b.Repository.Init(ctx); err != nil {
			return err
		}
	}
	if err := b.confirmTakingOver(ctx, yes); err != nil {
		return err
	}
	if err := b.backup(ctx, true); err != nil {
		return err
	}
	b.say(paint.Stdout.Success(fmt.Sprintf("This machine is now %s's main; backups from any other machine are refused.", b.Installation.Name)))
	return nil
}

func (b *Backups) confirmTakingOver(ctx context.Context, yes bool) error {
	state, latest, err := b.main(ctx)
	if err != nil {
		return fmt.Errorf("%w; nothing was claimed", err)
	}
	if state == thisMachine || yes {
		return nil
	}
	_, _ = fmt.Fprintf(b.ErrOut, "%s's main is %s. Taking over makes it refuse to back up.\n", b.Installation.Name, describe(latest))
	answer, interactive := b.Ask("Make this machine the main instead? (y/n) ")
	if !interactive {
		return errors.New("nothing was claimed; --yes takes over without asking")
	}
	if answer != "y" && answer != "Y" {
		return errors.New("nothing was claimed")
	}
	return nil
}

func (b *Backups) backup(ctx context.Context, claiming bool) error {
	if err := os.MkdirAll(b.Installation.Data, 0o755); err != nil { //nolint:gosec // the installation's data directory, read by its containers
		return err
	}
	lock, err := takeLock(filepath.Join(b.Installation.Data, ".backup.lock"))
	if err != nil {
		return err
	}
	defer lock.release()
	return b.backupHolding(ctx, lock, claiming)
}

func (b *Backups) backupHolding(ctx context.Context, lock *heldLock, claiming bool) (err error) {
	var stopped []string
	release := func() {}
	defer func() {
		err = b.finish(context.WithoutCancel(ctx), stopped, err)
		release()
	}()
	b.Pinger.Ping(ctx, "backup", "/start")
	if !claiming {
		if err := b.requireMain(ctx); err != nil {
			return err
		}
	}
	dir, err := b.dataToBackUp()
	if err != nil {
		return err
	}
	release = b.shielded()
	if stopped, err = b.stopStack(ctx); err != nil {
		return err
	}
	if err := b.snapshotVolumes(ctx, lock, dir); err != nil {
		return err
	}
	if err := b.startAgain(ctx, stopped); err != nil {
		return err
	}
	stopped = nil
	b.say("Removing old snapshots...")
	if err := b.Repository.Forget(ctx, b.Installation.Name, lock.files()); err != nil {
		return err
	}
	if err := markMain(b.Installation.Data); err != nil {
		return err
	}
	b.Pinger.Ping(ctx, "backup", "")
	b.say(paint.Stdout.Success("Backup done."))
	return nil
}

func (b *Backups) dataToBackUp() (string, error) {
	held, err := hasAppData(filepath.Join(b.Installation.Data, "volumes"))
	if err != nil {
		return "", err
	}
	if !held {
		return "", fmt.Errorf("volumes/ in %s holds no app data; nothing was backed up", b.Installation.Data)
	}
	return filepath.EvalSymlinks(b.Installation.Data)
}

func (b *Backups) stopStack(ctx context.Context) ([]string, error) {
	running, err := b.Stack.RunningServices(ctx)
	if err != nil {
		return nil, err
	}
	if err := b.Repository.Unlock(ctx); err != nil {
		return nil, err
	}
	b.say("Stopping the stack...")
	return running, b.Stack.Stop(ctx)
}

func (b *Backups) startAgain(ctx context.Context, services []string) error {
	if len(services) == 0 {
		return nil
	}
	b.say(fmt.Sprintf("Starting %d %s...", len(services), plural(len(services), "service")))
	return b.Stack.Start(ctx, services)
}

func (b *Backups) finish(ctx context.Context, stopped []string, err error) error {
	defer b.shielded()()
	if len(stopped) > 0 {
		if startErr := b.Stack.Start(ctx, stopped); startErr != nil {
			err = errors.Join(err, startErr)
		}
	}
	if err != nil {
		b.Pinger.Ping(ctx, "backup", "/fail")
	}
	return err
}

func (b *Backups) snapshotVolumes(ctx context.Context, lock *heldLock, dir string) error {
	b.say("Backing up volumes/...")
	return b.Repository.Backup(ctx, restic.BackupOptions{
		Host:        b.Installation.Name,
		Tags:        []string{"machine:" + b.MachineID, "machine-name:" + b.ShortHost, "nightly"},
		ExcludeFile: b.ExcludeFile,
		Dir:         dir,
		Paths:       []string{"volumes"},
		Inherit:     lock.files(),
	})
}

func (b *Backups) requireMain(ctx context.Context) error {
	state, _, err := b.main(ctx)
	if err != nil {
		return fmt.Errorf("%w; nothing was backed up", err)
	}
	if state == anotherMachine {
		if err := removeMarker(b.Installation.Data); err != nil {
			return err
		}
		return fmt.Errorf("another machine is %s's main; this machine does not back up (mse claim-backup-main makes it the main)", b.Installation.Name)
	}
	return nil
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
