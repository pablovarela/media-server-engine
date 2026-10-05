package restic

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/process"
)

var env = []string{"RESTIC_PASSWORD=p", "RESTIC_REPOSITORY=b2:bucket"}

func command(args ...string) process.Command {
	return process.Command{Name: "restic", Args: args, Env: env}
}

func TestCommands(t *testing.T) {
	held := os.NewFile(3, "held")
	type When struct {
		call func(r Restic) error
	}
	type Then struct {
		command process.Command
	}
	tests := map[string]struct {
		When When
		Then Then
	}{
		"init":       {When: When{call: func(r Restic) error { return r.Init(context.Background()) }}, Then: Then{command: command("init")}},
		"unlock":     {When: When{call: func(r Restic) error { return r.Unlock(context.Background()) }}, Then: Then{command: command("unlock")}},
		"unlock all": {When: When{call: func(r Restic) error { return r.UnlockAll(context.Background()) }}, Then: Then{command: command("unlock", "--remove-all")}},
		"check":      {When: When{call: func(r Restic) error { return r.Check(context.Background()) }}, Then: Then{command: command("check", "--retry-lock", "2h")}},
		"backup": {
			When: When{call: func(r Restic) error {
				return r.Backup(context.Background(), BackupOptions{Host: "gorgon", Tags: []string{"machine:abc", "nightly"}, ExcludeFile: "/state/excludes", Dir: "/data", Paths: []string{"volumes"}, Inherit: []*os.File{held}})
			}},
			Then: Then{command: process.Command{Name: "restic", Env: env, Dir: "/data", ExtraFiles: []*os.File{held},
				Args: []string{"backup", "--retry-lock", "2h", "--host", "gorgon", "--tag", "machine:abc", "--tag", "nightly", "--exclude-file", "/state/excludes", "volumes"}}},
		},
		"forget": {
			When: When{call: func(r Restic) error { return r.Forget(context.Background(), "gorgon", []*os.File{held}) }},
			Then: Then{command: process.Command{Name: "restic", Env: env, ExtraFiles: []*os.File{held},
				Args: []string{"forget", "--retry-lock", "2h", "--host", "gorgon", "--prune", "--keep-daily", "7", "--keep-weekly", "4", "--keep-monthly", "6"}}},
		},
		"restore with a host and filters": {
			When: When{call: func(r Restic) error {
				return r.Restore(context.Background(), RestoreOptions{Snapshot: "latest:/volumes", Host: "gorgon", Target: "/data/volumes", Include: []string{"*.db"}, Exclude: []string{"configarr"}})
			}},
			Then: Then{command: command("restore", "--retry-lock", "2h", "latest:/volumes", "--host", "gorgon", "--target", "/data/volumes", "--include", "*.db", "--exclude", "configarr")},
		},
		"restore any host": {
			When: When{call: func(r Restic) error {
				return r.Restore(context.Background(), RestoreOptions{Snapshot: "latest", Target: "/tmp/v"})
			}},
			Then: Then{command: command("restore", "--retry-lock", "2h", "latest", "--target", "/tmp/v")},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			runner := newMockRunner(t)
			runner.EXPECT().Run(mock.Anything, tt.Then.command).Return(0, nil)

			require.NoError(t, tt.When.call(Restic{Runner: runner, Env: env}))
		})
	}
}

func TestCommandFailures(t *testing.T) {
	t.Run("a failure names the command", func(t *testing.T) {
		runner := newMockRunner(t)
		runner.EXPECT().Run(mock.Anything, command("check", "--retry-lock", "2h")).Return(1, nil)

		assert.EqualError(t, Restic{Runner: runner, Env: env}.Check(context.Background()), "restic check failed (exit 1)")
	})
	t.Run("a lock lists who holds it", func(t *testing.T) {
		runner := newMockRunner(t)
		runner.EXPECT().Run(mock.Anything, command("check", "--retry-lock", "2h")).Return(11, nil)
		runner.EXPECT().Output(mock.Anything, command("list", "locks", "--no-lock")).Return(process.Result{Stdout: []byte("a1\n")}, nil)
		runner.EXPECT().Output(mock.Anything, command("cat", "lock", "a1", "--no-lock")).Return(process.Result{Stdout: []byte(`{"exclusive":true,"hostname":"pi","pid":42,"time":"2026-10-05T04:30:12.5+01:00"}`)}, nil)

		err := Restic{Runner: runner, Env: env}.Check(context.Background())

		assert.EqualError(t, err, "restic gave up waiting for a lock on the backup repository. Locks held:\n"+
			"  exclusive lock from pi (process 42) since 2026-10-05 04:30\n"+
			"If none of those machines is running restic now, remove every lock with: mse unlock-backup --all")
	})
	t.Run("a program that cannot start", func(t *testing.T) {
		runner := newMockRunner(t)
		runner.EXPECT().Run(mock.Anything, command("unlock")).Return(-1, errors.New(`exec: "restic": executable file not found in $PATH`))

		assert.EqualError(t, Restic{Runner: runner, Env: env}.Unlock(context.Background()), `exec: "restic": executable file not found in $PATH`)
	})
}

func TestHasRepository(t *testing.T) {
	tests := map[string]struct {
		Given struct{ result process.Result }
		Then  struct {
			exists bool
			err    string
		}
	}{
		"exists": {Then: struct {
			exists bool
			err    string
		}{exists: true}},
		"none": {Given: struct{ result process.Result }{process.Result{Exit: 10}}},
		"wrong password": {
			Given: struct{ result process.Result }{process.Result{Exit: 12, Stderr: []byte("Fatal: wrong password or no key found\n")}},
			Then: struct {
				exists bool
				err    string
			}{err: "restic cat failed (exit 12): Fatal: wrong password or no key found"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			runner := newMockRunner(t)
			runner.EXPECT().Output(mock.Anything, command("cat", "config")).Return(tt.Given.result, nil)

			exists, err := Restic{Runner: runner, Env: env}.HasRepository(context.Background())

			if tt.Then.err != "" {
				assert.EqualError(t, err, tt.Then.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.Then.exists, exists)
		})
	}
}

func TestSnapshots(t *testing.T) {
	type Then struct {
		snapshots []Snapshot
		err       string
	}
	tests := map[string]struct {
		Given struct{ result process.Result }
		Then  Then
	}{
		"parsed": {
			Given: struct{ result process.Result }{process.Result{Stdout: []byte(`[{"short_id":"1a2b","time":"2026-10-05T04:30:00+01:00","hostname":"gorgon","tags":["machine:abc","machine-name:pi","nightly"]}]`)}},
			Then:  Then{snapshots: []Snapshot{{ID: "1a2b", Time: time.Date(2026, 10, 5, 4, 30, 0, 0, time.FixedZone("", 3600)), Hostname: "gorgon", Tags: []string{"machine:abc", "machine-name:pi", "nightly"}}}},
		},
		"no repository": {Given: struct{ result process.Result }{process.Result{Exit: 10}}},
		"unreadable":    {Given: struct{ result process.Result }{process.Result{Exit: 1, Stderr: []byte("Fatal: unable to open repository\n")}}, Then: Then{err: "restic snapshots failed (exit 1): Fatal: unable to open repository"}},
		"bad JSON":      {Given: struct{ result process.Result }{process.Result{Stdout: []byte("not json")}}, Then: Then{err: "restic snapshots printed unreadable JSON: invalid character 'o' in literal null (expecting 'u')"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			runner := newMockRunner(t)
			runner.EXPECT().Output(mock.Anything, command("snapshots", "--no-lock", "--host", "gorgon", "--json")).Return(tt.Given.result, nil)

			snapshots, err := Restic{Runner: runner, Env: env}.Snapshots(context.Background(), "gorgon")

			if tt.Then.err != "" {
				assert.EqualError(t, err, tt.Then.err)
				return
			}
			require.NoError(t, err)
			require.Len(t, snapshots, len(tt.Then.snapshots))
			for n := range snapshots {
				assert.True(t, tt.Then.snapshots[n].Time.Equal(snapshots[n].Time))
				assert.Equal(t, tt.Then.snapshots[n].Tags, snapshots[n].Tags)
				assert.Equal(t, tt.Then.snapshots[n].ID, snapshots[n].ID)
			}
		})
	}
}

func TestSnapshotTag(t *testing.T) {
	snapshot := Snapshot{Tags: []string{"machine:abc", "nightly", "machine-name:pi:4"}}

	assert.Equal(t, "abc", snapshot.Tag("machine"))
	assert.Equal(t, "pi:4", snapshot.Tag("machine-name"))
	assert.Empty(t, snapshot.Tag("nightly"))
}

func TestLocks(t *testing.T) {
	runner := newMockRunner(t)
	runner.EXPECT().Output(mock.Anything, command("list", "locks", "--no-lock")).Return(process.Result{Stdout: []byte("a1\nb2\nc3\n")}, nil)
	runner.EXPECT().Output(mock.Anything, command("cat", "lock", "a1", "--no-lock")).Return(process.Result{Stdout: []byte(`{"exclusive":false,"hostname":"pi","pid":7,"time":"2026-10-05T04:30:00Z"}`)}, nil)
	runner.EXPECT().Output(mock.Anything, command("cat", "lock", "b2", "--no-lock")).Return(process.Result{Exit: 1}, nil)
	runner.EXPECT().Output(mock.Anything, command("cat", "lock", "c3", "--no-lock")).Return(process.Result{Stdout: []byte(`{"pid":9,"time":"2026-10-05T05:00:00Z"}`)}, nil)

	locks, err := Restic{Runner: runner, Env: env}.Locks(context.Background())

	require.NoError(t, err)
	require.Len(t, locks, 2)
	assert.Equal(t, "shared lock from pi (process 7) since 2026-10-05 04:30", locks[0].String())
	assert.Equal(t, "shared lock from an unknown host (process 9) since 2026-10-05 05:00", locks[1].String())
}
