package backup

import (
	"context"
	"fmt"
)

func (b *Backups) Unlock(ctx context.Context, all bool) error {
	title, unlock := "Removing stale locks", b.Repository.Unlock
	if all {
		title, unlock = "Removing every lock", b.Repository.UnlockAll
	}
	step := b.Report.Step(title)
	if err := unlock(ctx); err != nil {
		return step.Fail(err)
	}
	locks, err := b.Repository.Locks(ctx)
	if err != nil {
		return step.Fail(err)
	}
	if len(locks) == 0 {
		step.Done("no locks left")
		return nil
	}
	step.Done(fmt.Sprintf("%d left", len(locks)))
	for _, lock := range locks {
		b.Report.Say("  " + lock.String())
	}
	b.Report.Say("If none of those machines is running restic now, remove them with: mse unlock-backup --all")
	return nil
}
