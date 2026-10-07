package backup

import (
	"context"
	"os"
	"time"

	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/media"
	"github.com/pablovarela/media-server-engine/internal/report"
	"github.com/pablovarela/media-server-engine/internal/restic"
)

type Repository interface {
	HasRepository(ctx context.Context) (bool, error)
	Init(ctx context.Context) error
	Snapshots(ctx context.Context, host string) ([]restic.Snapshot, error)
	Unlock(ctx context.Context) error
	Backup(ctx context.Context, options restic.BackupOptions) (restic.BackupSummary, error)
	Forget(ctx context.Context, host string, keep restic.Keep, inherit []*os.File) (restic.ForgetSummary, error)
	Prune(ctx context.Context, inherit []*os.File) error
	Check(ctx context.Context, readDataSubset string) error
	Restore(ctx context.Context, options restic.RestoreOptions) error
}

type Stack interface {
	RunningServices(ctx context.Context) ([]string, error)
	AnyRunning(ctx context.Context) (bool, error)
	Stop(ctx context.Context) (string, error)
	Start(ctx context.Context, services []string) (string, error)
}

type Pinger interface {
	Ping(ctx context.Context, job, suffix string)
}

type Backups struct {
	Installation       *installation.Installation
	Repository         Repository
	RepositoryLocation string
	MediaRepository    Repository
	Media              media.Settings
	FreeSpace          func(path string) (uint64, error)
	Stack              Stack
	Pinger             Pinger
	MachineID          string
	ShortHost          string
	ExcludeFile        string
	TempDir            string
	Now                func() time.Time
	Ask                func(question string) (answer string, interactive bool)
	Shield             func() (release func())
	Report             *report.Reporter
}

func (b *Backups) shielded() (release func()) {
	if b.Shield == nil {
		return func() {}
	}
	return b.Shield()
}
