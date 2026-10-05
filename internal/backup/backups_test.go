package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/report"
	"github.com/pablovarela/media-server-engine/internal/restic"
)

type mocks struct {
	repository *mockRepository
	stack      *mockStack
	pinger     *mockPinger
}

func fixture(t *testing.T) (*Backups, mocks, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	m := mocks{repository: newMockRepository(t), stack: newMockStack(t), pinger: newMockPinger(t)}
	var out, errOut bytes.Buffer
	data := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(data, "volumes", "jellyfin"), 0o755))
	b := &Backups{
		Installation:       &installation.Installation{Name: "gorgon", Data: data, State: t.TempDir()},
		Repository:         m.repository,
		RepositoryLocation: "b2:bucket",
		Stack:              m.stack,
		Pinger:             m.pinger,
		MachineID:          "this",
		ShortHost:          "pi",
		ExcludeFile:        "/state/backup-excludes.txt",
		TempDir:            t.TempDir(),
		Now:                func() time.Time { return time.Date(2026, 10, 5, 4, 30, 0, 0, time.UTC) },
		Report:             report.New(&out, &errOut, nil),
	}
	return b, m, &out, &errOut
}

func snapshot(machine, name, at string) restic.Snapshot {
	when, _ := time.Parse("2006-01-02 15:04", at)
	return restic.Snapshot{Time: when, Hostname: "gorgon", Tags: []string{"machine:" + machine, "machine-name:" + name, "nightly"}}
}
