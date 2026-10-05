package backup

import (
	"context"
	"fmt"

	"github.com/pablovarela/media-server-engine/internal/restic"
)

type mainState int

const (
	thisMachine mainState = iota
	anotherMachine
)

func (b *Backups) main(ctx context.Context) (mainState, *restic.Snapshot, error) {
	snapshots, err := b.Repository.Snapshots(ctx, b.Installation.Name)
	if err != nil {
		return thisMachine, nil, fmt.Errorf("cannot read the backup repository to tell which machine is %s's main (%w)", b.Installation.Name, err)
	}
	var latest *restic.Snapshot
	for n := range snapshots {
		if latest == nil || snapshots[n].Time.After(latest.Time) {
			latest = &snapshots[n]
		}
	}
	if latest != nil {
		if machine := latest.Tag("machine"); machine != "" && machine != b.MachineID {
			return anotherMachine, latest, nil
		}
	}
	return thisMachine, latest, nil
}

func describe(s *restic.Snapshot) string {
	name := s.Tag("machine-name")
	if name == "" {
		name = "machine " + s.Tag("machine")
	}
	return fmt.Sprintf("%s, last backup %s", name, s.Time.Format("2006-01-02 15:04"))
}

func (b *Backups) ownHost(ctx context.Context) string {
	if snapshots, err := b.Repository.Snapshots(ctx, b.Installation.Name); err == nil && len(snapshots) > 0 {
		return b.Installation.Name
	}
	return ""
}

func (b *Backups) DescribeRole(ctx context.Context) error {
	state, latest, err := b.main(ctx)
	if err != nil {
		return err
	}
	name := b.Installation.Name
	if latest == nil {
		b.say(name + " has no backups yet; the first machine to back up becomes its main.")
		return nil
	}
	b.say(fmt.Sprintf("%s's main is %s.", name, describe(latest)))
	if state == anotherMachine {
		b.say("This machine is not the main; mse claim-backup-main makes it the main.")
		return nil
	}
	b.say("This machine is the main.")
	return nil
}
