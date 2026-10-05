package backup

import "context"

func (b *Backups) Unlock(ctx context.Context, all bool) error {
	unlock := b.Repository.Unlock
	if all {
		unlock = b.Repository.UnlockAll
	}
	if err := unlock(ctx); err != nil {
		return err
	}
	locks, err := b.Repository.Locks(ctx)
	if err != nil {
		return err
	}
	if len(locks) == 0 {
		b.say("no locks left on the backup repository")
		return nil
	}
	b.say("Locks left, held by restic processes that may still be running:")
	for _, lock := range locks {
		b.say("  " + lock.String())
	}
	b.say("If none of those machines is running restic now, remove them with: mse unlock-backup --all")
	return nil
}
