package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/restic"
)

type Repository interface {
	HasRepository(ctx context.Context) (bool, error)
	Init(ctx context.Context) error
	Snapshots(ctx context.Context, host string) ([]restic.Snapshot, error)
	Unlock(ctx context.Context) error
	UnlockAll(ctx context.Context) error
	Locks(ctx context.Context) ([]restic.Lock, error)
	Backup(ctx context.Context, options restic.BackupOptions) error
	Forget(ctx context.Context, host string, inherit []*os.File) error
	Check(ctx context.Context) error
	Restore(ctx context.Context, options restic.RestoreOptions) error
}

type Stack interface {
	RunningServices(ctx context.Context) ([]string, error)
	Stop(ctx context.Context) error
	Start(ctx context.Context, services []string) error
}

type Pinger interface {
	Ping(ctx context.Context, job, suffix string)
}

type Backups struct {
	Installation       *installation.Installation
	Repository         Repository
	RepositoryLocation string
	Stack              Stack
	Pinger             Pinger
	MachineID          string
	ShortHost          string
	ExcludeFile        string
	TempDir            string
	Now                func() time.Time
	Ask                func(question string) (answer string, interactive bool)
	Out                io.Writer
	ErrOut             io.Writer
}

func (b *Backups) say(line string) {
	_, _ = fmt.Fprintln(b.Out, line)
}
