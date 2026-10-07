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
		step := b.Report.Step("Creating the backup repository " + b.RepositoryLocation)
		if err := b.Repository.Init(ctx); err != nil {
			return step.Fail(err)
		}
		step.Done("created")
	}
	latest, err := b.confirmTakingOver(ctx, yes)
	if err != nil {
		return err
	}
	held, err := b.HasAppData()
	if err != nil {
		return err
	}
	if !held && latest != nil {
		return fmt.Errorf("volumes/ in %s holds no app data, so claiming would make an empty backup %s's latest; restore first with mse restore --apps, then mse backup --apps --take-over", b.Installation.Data, b.Installation.Name)
	}
	if err := b.backup(ctx, true); err != nil {
		return err
	}
	b.Report.Say(paint.Stdout.Success(fmt.Sprintf("This machine is now %s's main; backups from any other machine are refused.", b.Installation.Name)))
	return nil
}

func (b *Backups) confirmTakingOver(ctx context.Context, yes bool) (*restic.Snapshot, error) {
	state, latest, err := b.main(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w; nothing was claimed", err)
	}
	if state == thisMachine || yes {
		return latest, nil
	}
	b.Report.Warn(fmt.Sprintf("%s's main is %s. Taking over makes it refuse to back up.", b.Installation.Name, describe(latest)))
	answer, interactive := b.Ask("Make this machine the main instead? (y/n) ")
	if !interactive {
		return nil, errors.New("nothing was claimed; mse backup --apps --take-over --yes takes over without asking")
	}
	if answer != "y" && answer != "Y" {
		return nil, errors.New("nothing was claimed")
	}
	return latest, nil
}

func (b *Backups) backup(ctx context.Context, claiming bool) error {
	if err := os.MkdirAll(b.Installation.Data, 0o755); err != nil { //nolint:gosec // the installation's data directory, read by its containers
		return err
	}
	lock, err := takeLock(LockPath(b.Installation.Data))
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
	if !claiming {
		_, refused, err := b.refuseOnCopy(ctx, "backup", "This machine doesn't back up; mse backup --apps --take-over makes it the main and backs up now.")
		if err != nil {
			return fmt.Errorf("%w; nothing was backed up", err)
		}
		if refused {
			return nil
		}
	}
	b.Pinger.Ping(ctx, "backup", "/start")
	dir, err := b.dataToBackUp(claiming)
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
	if err := b.removeOldSnapshots(ctx, lock); err != nil {
		return err
	}
	if err := markMain(b.Installation.Data); err != nil {
		return err
	}
	b.Pinger.Ping(ctx, "backup", "")
	b.Report.Say(paint.Stdout.Success("Backup done."))
	return nil
}

func (b *Backups) dataToBackUp(claiming bool) (string, error) {
	volumes := filepath.Join(b.Installation.Data, "volumes")
	held, err := hasAppData(volumes)
	if err != nil {
		return "", err
	}
	if claiming {
		if err := os.MkdirAll(volumes, 0o755); err != nil { //nolint:gosec // the apps' volumes, read by their containers
			return "", err
		}
	}
	if !held && !claiming {
		return "", fmt.Errorf("volumes/ in %s holds no app data; nothing was backed up", b.Installation.Data)
	}
	return filepath.EvalSymlinks(b.Installation.Data)
}

func (b *Backups) stopStack(ctx context.Context) ([]string, error) {
	step := b.Report.Step("Stopping the stack")
	running, err := b.Stack.RunningServices(ctx)
	if err != nil {
		return nil, step.Fail(err)
	}
	if err := b.Repository.Unlock(ctx); err != nil {
		return nil, step.Fail(err)
	}
	result, err := b.Stack.Stop(ctx)
	if err != nil {
		return running, step.Fail(err)
	}
	step.Done(result)
	return running, nil
}

func (b *Backups) startAgain(ctx context.Context, services []string) error {
	if len(services) == 0 {
		return nil
	}
	step := b.Report.Step("Starting the services again")
	result, err := b.Stack.Start(ctx, services)
	if err != nil {
		return step.Fail(err)
	}
	step.Done(result)
	return nil
}

func (b *Backups) finish(ctx context.Context, stopped []string, err error) error {
	defer b.shielded()()
	if startErr := b.startAgain(ctx, stopped); startErr != nil {
		err = errors.Join(err, startErr)
	}
	if err != nil {
		b.Pinger.Ping(ctx, "backup", "/fail")
	}
	return err
}

func (b *Backups) snapshotVolumes(ctx context.Context, lock *heldLock, dir string) error {
	step := b.Report.Step("Backing up volumes/")
	summary, err := b.Repository.Backup(ctx, restic.BackupOptions{
		Host:        b.Installation.Name,
		Tags:        []string{"machine:" + b.MachineID, "machine-name:" + b.ShortHost, "nightly"},
		ExcludeFile: b.ExcludeFile,
		Dir:         dir,
		Paths:       []string{"volumes"},
		Inherit:     lock.files(),
	})
	if err != nil {
		return step.Fail(err)
	}
	step.Done(summary.String())
	return nil
}

func (b *Backups) removeOldSnapshots(ctx context.Context, lock *heldLock) error {
	step := b.Report.Step("Removing old snapshots")
	summary, err := b.Repository.Forget(ctx, b.Installation.Name, lock.files())
	if err != nil {
		return step.Fail(err)
	}
	if len(summary.Removed) == 0 {
		step.Done(summary.String())
		return nil
	}
	if err := b.Repository.Prune(ctx, lock.files()); err != nil {
		return step.Fail(err)
	}
	step.Done(summary.String() + "; pruned")
	return nil
}

func (b *Backups) refuseOnCopy(ctx context.Context, job, instead string) (*restic.Snapshot, bool, error) {
	state, latest, err := b.main(ctx)
	if err != nil {
		b.Pinger.Ping(ctx, job, "/start")
		return nil, false, err
	}
	if state != anotherMachine {
		return latest, false, nil
	}
	if err := removeMarker(b.Installation.Data); err != nil {
		return nil, false, err
	}
	b.Report.Say(fmt.Sprintf("%s's main is %s. %s", b.Installation.Name, describe(latest), instead))
	return latest, true, nil
}
