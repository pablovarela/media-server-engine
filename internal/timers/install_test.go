package timers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/process"
)

type systemctl struct {
	calls []string
}

func recording(t *testing.T, failing map[string]string, before func(call string)) (*mockRunner, *systemctl) {
	t.Helper()
	s := &systemctl{}
	runner := newMockRunner(t)
	runner.EXPECT().Output(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, c process.Command) (process.Result, error) {
		require.Equal(t, "systemctl", c.Name)
		call := strings.Join(c.Args, " ")
		if before != nil {
			before(call)
		}
		s.calls = append(s.calls, call)
		if stderr, failed := failing[call]; failed {
			return process.Result{Exit: 1, Stderr: []byte(stderr)}, nil
		}
		return process.Result{}, nil
	}).Maybe()
	return runner, s
}

func place(t *testing.T, dir string, jobs ...Job) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for _, job := range jobs {
		for _, suffix := range []string{".service", ".timer"} {
			text, err := Render(job, suffix, gorgon)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, job.Unit("gorgon")+suffix), []byte(text), 0o644))
		}
	}
}

const enableAll = "--user enable --now mse-gorgon-update.timer mse-gorgon-download-cleanup.timer mse-gorgon-backup.timer mse-gorgon-verify.timer"

func TestNothingChangedWritesNothingAndStillEnablesTheTimers(t *testing.T) {
	dir := t.TempDir()
	place(t, dir, Update, Cleanup, Backup, Verify)
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(filepath.Join(dir, "mse-gorgon-update.service"), old, old))
	runner, s := recording(t, nil, nil)

	outcome, err := Installer{Runner: runner, Dir: dir}.Install(context.Background(), Main, false, gorgon)

	require.NoError(t, err)
	assert.Equal(t, []string{"--user daemon-reload", enableAll}, s.calls)
	assert.Equal(t, "all 4 unchanged", outcome.String())
	info, err := os.Stat(filepath.Join(dir, "mse-gorgon-update.service"))
	require.NoError(t, err)
	assert.Equal(t, old, info.ModTime().UTC(), "an unchanged unit isn't written")
}

func TestAChangedUnitIsRewritten(t *testing.T) {
	dir := t.TempDir()
	place(t, dir, Update, Cleanup, Backup, Verify)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mse-gorgon-update.service"), []byte("[Service]\nExecStart=/old/mse update\n"), 0o644))
	runner, s := recording(t, nil, nil)

	outcome, err := Installer{Runner: runner, Dir: dir}.Install(context.Background(), Main, false, gorgon)

	require.NoError(t, err)
	assert.Equal(t, []string{"--user daemon-reload", enableAll}, s.calls)
	written, err := os.ReadFile(filepath.Join(dir, "mse-gorgon-update.service"))
	require.NoError(t, err)
	want, err := Render(Update, ".service", gorgon)
	require.NoError(t, err)
	assert.Equal(t, want, string(written))
	assert.Equal(t, "mse-gorgon-update changed; mse-gorgon-download-cleanup, mse-gorgon-backup, mse-gorgon-verify unchanged", outcome.String())
}

func TestAFreshMachineGetsItsUnitDirectoryAndEveryFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "systemd", "user")
	runner, _ := recording(t, nil, nil)

	outcome, err := Installer{Runner: runner, Dir: dir}.Install(context.Background(), Secondary, false, gorgon)

	require.NoError(t, err)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 4)
	assert.Equal(t, "mse-gorgon-update, mse-gorgon-download-cleanup changed", outcome.String())
}

func TestAFormerMainDisablesItsBackupTimersBeforeRemovingThem(t *testing.T) {
	dir := t.TempDir()
	place(t, dir, Update, Cleanup, Backup, Verify)
	runner, s := recording(t, nil, func(call string) {
		if strings.HasPrefix(call, "--user disable") {
			assert.FileExists(t, filepath.Join(dir, strings.TrimPrefix(call, "--user disable --now ")))
		}
	})

	outcome, err := Installer{Runner: runner, Dir: dir}.Install(context.Background(), Secondary, false, gorgon)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"--user disable --now mse-gorgon-backup.timer",
		"--user disable --now mse-gorgon-verify.timer",
		"--user daemon-reload",
		"--user enable --now mse-gorgon-update.timer mse-gorgon-download-cleanup.timer",
	}, s.calls)
	for _, file := range append(Backup.Files("gorgon"), Verify.Files("gorgon")...) {
		assert.NoFileExists(t, filepath.Join(dir, file))
	}
	assert.Equal(t, "mse-gorgon-update, mse-gorgon-download-cleanup unchanged; mse-gorgon-backup, mse-gorgon-verify removed", outcome.String())
}

func TestALeftoverServiceWithoutItsTimerIsRemovedToo(t *testing.T) {
	dir := t.TempDir()
	place(t, dir, Update, Cleanup)
	text, err := Render(Backup, ".service", gorgon)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mse-gorgon-backup.service"), []byte(text), 0o644))
	runner, s := recording(t, nil, nil)

	outcome, err := Installer{Runner: runner, Dir: dir}.Install(context.Background(), Secondary, false, gorgon)

	require.NoError(t, err)
	assert.NotContains(t, s.calls, "--user disable --now mse-gorgon-backup.timer", "systemctl can't disable a timer whose file is gone")
	assert.NoFileExists(t, filepath.Join(dir, "mse-gorgon-backup.service"))
	assert.Equal(t, []string{"mse-gorgon-backup"}, outcome.Removed)
}

func TestAnUnknownMainLeavesTheBackupTimersAsTheyAre(t *testing.T) {
	dir := t.TempDir()
	place(t, dir, Update, Cleanup)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mse-gorgon-backup.service"), []byte("an older backup unit\n"), 0o644))
	runner, s := recording(t, nil, nil)

	outcome, err := Installer{Runner: runner, Dir: dir}.Install(context.Background(), Unknown, false, gorgon)

	require.NoError(t, err)
	kept, err := os.ReadFile(filepath.Join(dir, "mse-gorgon-backup.service"))
	require.NoError(t, err)
	assert.Equal(t, "an older backup unit\n", string(kept))
	assert.NotContains(t, strings.Join(s.calls, "\n"), "backup")
	assert.Equal(t, "all 2 unchanged", outcome.String())
}

func TestAFailedSystemctlStopsWithItsMessage(t *testing.T) {
	runner, s := recording(t, map[string]string{"--user daemon-reload": "Failed to connect to bus: No medium found\n"}, nil)

	_, err := Installer{Runner: runner, Dir: t.TempDir()}.Install(context.Background(), Main, false, gorgon)

	assert.EqualError(t, err, "systemctl --user daemon-reload failed: Failed to connect to bus: No medium found")
	assert.Equal(t, []string{"--user daemon-reload"}, s.calls)
}

func TestAFailureWithoutAMessageNamesTheExitCode(t *testing.T) {
	runner, _ := recording(t, map[string]string{enableAll: ""}, nil)

	_, err := Installer{Runner: runner, Dir: t.TempDir()}.Install(context.Background(), Main, false, gorgon)

	assert.EqualError(t, err, "systemctl "+enableAll+" failed: exit 1")
}

func TestAMainWithAMediaBackupGetsItsTimer(t *testing.T) {
	dir := t.TempDir()
	runner, s := recording(t, nil, nil)

	outcome, err := Installer{Runner: runner, Dir: dir}.Install(context.Background(), Main, true, gorgon)

	require.NoError(t, err)
	assert.Contains(t, s.calls, enableAll+" mse-gorgon-media-backup.timer")
	timer, err := os.ReadFile(filepath.Join(dir, "mse-gorgon-media-backup.timer"))
	require.NoError(t, err)
	assert.Contains(t, string(timer), "OnCalendar=Sun *-*-* 01:00:00\n")
	assert.Contains(t, outcome.Changed, "mse-gorgon-media-backup")
}

func TestTurningTheMediaBackupOffRemovesItsTimer(t *testing.T) {
	dir := t.TempDir()
	place(t, dir, Update, Cleanup, Backup, Verify, MediaBackup)
	runner, s := recording(t, nil, nil)

	outcome, err := Installer{Runner: runner, Dir: dir}.Install(context.Background(), Main, false, gorgon)

	require.NoError(t, err)
	assert.Contains(t, s.calls, "--user disable --now mse-gorgon-media-backup.timer")
	assert.NoFileExists(t, filepath.Join(dir, "mse-gorgon-media-backup.timer"))
	assert.Equal(t, "mse-gorgon-update, mse-gorgon-download-cleanup, mse-gorgon-backup, mse-gorgon-verify unchanged; mse-gorgon-media-backup removed", outcome.String())
}
