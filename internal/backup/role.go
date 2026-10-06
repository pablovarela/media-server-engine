package backup

import (
	"context"
	"fmt"
	"time"

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
	return fmt.Sprintf("%s, last backup %s", machineName(s), s.Time.Format("2006-01-02 15:04"))
}

func machineName(s *restic.Snapshot) string {
	if name := s.Tag("machine-name"); name != "" {
		return name
	}
	return "machine " + s.Tag("machine")
}

type Main struct {
	Machine     string
	Time        time.Time
	ThisMachine bool
}

func (b *Backups) CurrentMain(ctx context.Context) (*Main, error) {
	state, latest, err := b.main(ctx)
	if err != nil || latest == nil {
		return nil, err
	}
	return &Main{Machine: machineName(latest), Time: latest.Time, ThisMachine: state == thisMachine}, nil
}

func (b *Backups) ownHost(ctx context.Context) (string, error) {
	snapshots, err := b.Repository.Snapshots(ctx, b.Installation.Name)
	if err != nil || len(snapshots) == 0 {
		return "", err
	}
	return b.Installation.Name, nil
}

func (b *Backups) DescribeRole(ctx context.Context) error {
	state, latest, err := b.main(ctx)
	if err != nil {
		return err
	}
	name := b.Installation.Name
	if latest == nil {
		b.Report.Say(name + " has no backups yet; the first machine to back up becomes its main.")
		return nil
	}
	b.Report.Say(fmt.Sprintf("%s's main is %s.", name, describe(latest)))
	if state == anotherMachine {
		b.Report.Say("This machine is not the main; mse claim-backup-main makes it the main.")
		return nil
	}
	b.Report.Say("This machine is the main.")
	return nil
}

func (b *Backups) RunsBackups(ctx context.Context) (runs, backedUp bool, err error) {
	state, latest, err := b.main(ctx)
	switch {
	case err != nil:
		return false, false, err
	case latest == nil:
		return b.Installation.Role() == "main", false, nil
	case state == anotherMachine:
		return false, true, removeMarker(b.Installation.Data)
	}
	return true, true, markMain(b.Installation.Data)
}
