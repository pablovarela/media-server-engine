package restic

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/process"
)

var env = []string{"RESTIC_PASSWORD=p", "RESTIC_REPOSITORY=b2:bucket"}

const binary = "/cache/restic"

func command(args ...string) process.Command {
	return process.Command{Name: binary, Args: args, Env: env}
}

func TestCommands(t *testing.T) {
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
		"init":   {When: When{call: func(r Restic) error { return r.Init(context.Background()) }}, Then: Then{command: command("init")}},
		"unlock": {When: When{call: func(r Restic) error { return r.Unlock(context.Background()) }}, Then: Then{command: command("unlock")}},
		"check":  {When: When{call: func(r Restic) error { return r.Check(context.Background(), "") }}, Then: Then{command: command("check", "--retry-lock", "2h")}},
		"restore with a host and filters": {
			When: When{call: func(r Restic) error {
				return r.Restore(context.Background(), RestoreOptions{Snapshot: "latest:/volumes", Host: "gorgon", Target: "/data/volumes", Include: []string{"*.db"}, Exclude: []string{"configarr"}})
			}},
			Then: Then{command: command("restore", "--retry-lock", "2h", "latest:/volumes", "--host", "gorgon", "--target", "/data/volumes", "--include", "*.db", "--exclude", "configarr")},
		},
		"check a sample": {
			When: When{call: func(r Restic) error { return r.Check(context.Background(), "5%") }},
			Then: Then{command: command("check", "--retry-lock", "2h", "--read-data-subset", "5%")},
		},
		"restore over what is there when it changed": {
			When: When{call: func(r Restic) error {
				return r.Restore(context.Background(), RestoreOptions{Snapshot: "latest", Host: "gorgon", Target: "/data/media", Overwrite: "if-changed"})
			}},
			Then: Then{command: command("restore", "--retry-lock", "2h", "latest", "--host", "gorgon", "--target", "/data/media", "--overwrite", "if-changed")},
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

			require.NoError(t, tt.When.call(Restic{Binary: binary, Runner: runner, Env: env}))
		})
	}
}

func TestCommandFailures(t *testing.T) {
	t.Run("a failure names the command", func(t *testing.T) {
		runner := newMockRunner(t)
		runner.EXPECT().Run(mock.Anything, command("check", "--retry-lock", "2h")).Return(1, nil)

		assert.EqualError(t, Restic{Binary: binary, Runner: runner, Env: env}.Check(context.Background(), ""), "restic check failed (exit 1)")
	})
	t.Run("a lock lists who holds it", func(t *testing.T) {
		runner := newMockRunner(t)
		runner.EXPECT().Run(mock.Anything, command("check", "--retry-lock", "2h")).Return(11, nil)
		runner.EXPECT().Output(mock.Anything, command("list", "locks", "--no-lock")).Return(process.Result{Stdout: []byte("a1\n")}, nil)
		runner.EXPECT().Output(mock.Anything, command("cat", "lock", "a1", "--no-lock")).Return(process.Result{Stdout: []byte(`{"exclusive":true,"hostname":"pi","pid":42,"time":"2026-10-05T04:30:12.5+01:00"}`)}, nil)

		err := Restic{Binary: binary, Runner: runner, Env: env}.Check(context.Background(), "")

		assert.EqualError(t, err, "restic gave up waiting for a lock on the backup repository. Locks held:\n"+
			"  exclusive lock from pi (process 42) since 2026-10-05 04:30\n"+
			"Each of those locks is still being refreshed, so restic is running on that machine: let it finish, or stop it there, then run this again.")
	})
	t.Run("a program that cannot start", func(t *testing.T) {
		runner := newMockRunner(t)
		runner.EXPECT().Run(mock.Anything, command("unlock")).Return(-1, errors.New(`exec: "restic": executable file not found in $PATH`))

		assert.EqualError(t, Restic{Binary: binary, Runner: runner, Env: env}.Unlock(context.Background()), `exec: "restic": executable file not found in $PATH`)
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
			runner.EXPECT().Output(mock.Anything, command("cat", "config", "--no-lock")).Return(tt.Given.result, nil)

			exists, err := Restic{Binary: binary, Runner: runner, Env: env}.HasRepository(context.Background())

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

			snapshots, err := Restic{Binary: binary, Runner: runner, Env: env}.Snapshots(context.Background(), "gorgon")

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
	runner.EXPECT().Output(mock.Anything, command("cat", "lock", "c3", "--no-lock")).Return(process.Result{Stdout: []byte(`{"time":"2026-10-05T05:00:00Z"}`)}, nil)

	locks, err := Restic{Binary: binary, Runner: runner, Env: env}.Locks(context.Background())

	require.NoError(t, err)
	require.Len(t, locks, 2)
	assert.Equal(t, "shared lock from pi (process 7) since 2026-10-05 04:30", locks[0].String())
	assert.Equal(t, "shared lock from an unknown host (process ?) since 2026-10-05 05:00", locks[1].String())
}

func TestBackupSummary(t *testing.T) {
	type Given struct {
		stdout string
		stderr string
	}
	type Then struct {
		summary BackupSummary
		logged  string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"progress is dropped and the summary read": {
			Given: Given{stdout: `{"message_type":"status","percent_done":0.5}` + "\n" +
				`{"message_type":"summary","files_new":2,"files_changed":877,"files_unmodified":803,"data_added":21472870,"data_added_packed":2988441,"snapshot_id":"40c4a929f0d1e2b3"}` + "\n"},
			Then: Then{
				summary: BackupSummary{SnapshotID: "40c4a929f0d1e2b3", FilesNew: 2, FilesChanged: 877, FilesUnmodified: 803, DataAdded: 21472870, DataAddedPacked: 2988441},
				logged:  "snapshot 40c4a929: 2 new, 877 changed, 803 unchanged files; 20.5 MiB added (2.8 MiB stored)\n",
			},
		},
		"errors on stderr are kept readably": {
			Given: Given{
				stdout: `{"message_type":"summary","snapshot_id":"40c4a929f0d1e2b3"}` + "\n",
				stderr: `{"message_type":"error","error":{"message":"permission denied"},"during":"archival","item":"/data/volumes/x"}` + "\n" +
					`{"message_type":"exit_error","code":3,"message":"Warning: at least one source file could not be read"}` + "\n" +
					"a line that is not JSON\n",
			},
			Then: Then{
				summary: BackupSummary{SnapshotID: "40c4a929f0d1e2b3"},
				logged: "snapshot 40c4a929: 0 new, 0 changed, 0 unchanged files; 0 B added (0 B stored)\n" +
					"error during archival: /data/volumes/x: permission denied\n" +
					"Warning: at least one source file could not be read\n" +
					"a line that is not JSON\n",
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var logged bytes.Buffer
			runner := newMockRunner(t)
			runner.EXPECT().Run(mock.Anything, mock.MatchedBy(func(c process.Command) bool {
				return slices.Equal(c.Args, []string{"backup", "--json", "--retry-lock", "2h", "--host", "gorgon", "--tag", "nightly", "--exclude-file", "/state/excludes", "volumes"}) && c.Dir == "/data" && c.Stdout != nil && c.Stderr != nil
			})).RunAndReturn(func(_ context.Context, c process.Command) (int, error) {
				_, _ = io.WriteString(c.Stdout, tt.Given.stdout)
				_, _ = io.WriteString(c.Stderr, tt.Given.stderr)
				return 0, nil
			})

			summary, err := Restic{Binary: binary, Runner: runner, Env: env, Log: &logged}.Backup(context.Background(), BackupOptions{Host: "gorgon", Tags: []string{"nightly"}, ExcludeFile: "/state/excludes", Dir: "/data", Paths: []string{"volumes"}})

			require.NoError(t, err)
			assert.Equal(t, tt.Then.summary, summary)
			assert.Equal(t, tt.Then.logged, logged.String())
		})
	}
}

func TestForgetSummary(t *testing.T) {
	var logged bytes.Buffer
	runner := newMockRunner(t)
	runner.EXPECT().Run(mock.Anything, mock.MatchedBy(func(c process.Command) bool {
		return slices.Equal(c.Args, []string{"forget", "--json", "--retry-lock", "2h", "--host", "gorgon", "--keep-daily", "7", "--keep-weekly", "4", "--keep-monthly", "6"}) && c.Stdout != nil
	})).RunAndReturn(func(_ context.Context, c process.Command) (int, error) {
		_, _ = io.WriteString(c.Stdout, `[{"keep":[{"short_id":"a"},{"short_id":"b"}],"remove":[{"short_id":"c","time":"2026-10-05T04:31:01+01:00"}]}]`)
		return 0, nil
	})

	summary, err := Restic{Binary: binary, Runner: runner, Env: env, Log: &logged}.Forget(context.Background(), "gorgon", Keep{Daily: 7, Weekly: 4, Monthly: 6}, nil)

	require.NoError(t, err)
	assert.Equal(t, "kept 2, removed 1 (2026-10-05 04:31)", summary.String())
	assert.Equal(t, "kept 2, removed 1 (2026-10-05 04:31)\n", logged.String())
}

func TestPrune(t *testing.T) {
	runner := newMockRunner(t)
	runner.EXPECT().Run(mock.Anything, command("prune", "--retry-lock", "2h")).Return(0, nil)

	require.NoError(t, Restic{Binary: binary, Runner: runner, Env: env}.Prune(context.Background(), nil))
}

func TestForgetWithNothingToForget(t *testing.T) {
	runner := newMockRunner(t)
	runner.EXPECT().Run(mock.Anything, mock.MatchedBy(func(c process.Command) bool { return slices.Contains(c.Args, "forget") })).Return(0, nil)

	summary, err := Restic{Binary: binary, Runner: runner, Env: env}.Forget(context.Background(), "gorgon", Keep{Daily: 7, Weekly: 4, Monthly: 6}, nil)

	require.NoError(t, err)
	assert.Equal(t, "kept 0, removed 0", summary.String())
}

func TestBackupArguments(t *testing.T) {
	tests := map[string]struct {
		options BackupOptions
		args    []string
	}{
		"apps": {
			options: BackupOptions{Host: "gorgon", Tags: []string{"nightly"}, ExcludeFile: "/x.txt", Paths: []string{"volumes"}},
			args:    []string{"backup", "--json", "--retry-lock", "2h", "--host", "gorgon", "--tag", "nightly", "--exclude-file", "/x.txt", "volumes"},
		},
		"media with an upload cap": {
			options: BackupOptions{Host: "gorgon", Tags: []string{"weekly"}, LimitUpload: 2048, Paths: []string{"."}},
			args:    []string{"backup", "--json", "--retry-lock", "2h", "--host", "gorgon", "--tag", "weekly", "--limit-upload", "2048", "."},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			runner := newMockRunner(t)
			runner.EXPECT().Run(mock.Anything, mock.MatchedBy(func(c process.Command) bool { return slices.Equal(tt.args, c.Args) })).Return(0, nil)

			_, err := Restic{Binary: binary, Runner: runner, Env: env}.Backup(context.Background(), tt.options)

			require.NoError(t, err)
		})
	}
}

func TestForgetKeeps(t *testing.T) {
	tests := map[string]struct {
		keep Keep
		args []string
	}{
		"weekly only":            {keep: Keep{Weekly: 4}, args: []string{"--keep-weekly", "4"}},
		"daily, weekly, monthly": {keep: Keep{Daily: 7, Weekly: 4, Monthly: 6}, args: []string{"--keep-daily", "7", "--keep-weekly", "4", "--keep-monthly", "6"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			want := append([]string{"forget", "--json", "--retry-lock", "2h", "--host", "gorgon"}, tt.args...)
			runner := newMockRunner(t)
			runner.EXPECT().Run(mock.Anything, mock.MatchedBy(func(c process.Command) bool { return slices.Equal(want, c.Args) })).Return(0, nil)

			_, err := Restic{Binary: binary, Runner: runner, Env: env}.Forget(context.Background(), "gorgon", tt.keep, nil)

			require.NoError(t, err)
		})
	}
}

func TestBackupReportsProgress(t *testing.T) {
	runner := newMockRunner(t)
	runner.EXPECT().Run(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, c process.Command) (int, error) {
		_, _ = io.WriteString(c.Stdout, `{"message_type":"status","percent_done":0.1,"total_bytes":45000000000,"bytes_done":4500000000}`+"\n"+
			`{"message_type":"summary","snapshot_id":"7d2e9c41aa00"}`+"\n")
		return 0, nil
	})
	var seen []Progress

	_, err := Restic{Binary: binary, Runner: runner, Env: env}.Backup(context.Background(), BackupOptions{Host: "gorgon", Paths: []string{"."}, Progress: func(p Progress) { seen = append(seen, p) }})

	require.NoError(t, err)
	assert.Equal(t, []Progress{{Done: 0.1, TotalBytes: 45000000000, BytesDone: 4500000000}}, seen)
}

func TestRestoreReportsProgress(t *testing.T) {
	var logged bytes.Buffer
	runner := newMockRunner(t)
	runner.EXPECT().Run(mock.Anything, mock.MatchedBy(func(c process.Command) bool {
		return slices.Equal([]string{"restore", "--retry-lock", "2h", "--json", "latest", "--host", "gorgon", "--target", "/data/media", "--overwrite", "if-changed"}, c.Args)
	})).RunAndReturn(func(_ context.Context, c process.Command) (int, error) {
		_, _ = io.WriteString(c.Stdout, `{"message_type":"status","percent_done":0.5,"total_bytes":400,"bytes_restored":200}`+"\n"+
			`{"message_type":"summary","total_files":1,"files_restored":1,"total_bytes":400,"bytes_restored":400}`+"\n")
		return 0, nil
	})
	var seen []Progress

	err := Restic{Binary: binary, Runner: runner, Env: env, Log: &logged}.Restore(context.Background(), RestoreOptions{
		Snapshot: "latest", Host: "gorgon", Target: "/data/media", Overwrite: "if-changed", Progress: func(p Progress) { seen = append(seen, p) },
	})

	require.NoError(t, err)
	assert.Equal(t, []Progress{{Done: 0.5, TotalBytes: 400, BytesDone: 200}}, seen)
	assert.Contains(t, logged.String(), `"files_restored":1`)
}
