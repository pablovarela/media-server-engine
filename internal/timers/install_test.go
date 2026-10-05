package timers

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/process"
)

const dir = "/etc/systemd/system"

type sudo struct {
	calls   []string
	written map[string]string
}

func recording(t *testing.T, failing map[string]string) (*mockRunner, *sudo) {
	t.Helper()
	s := &sudo{written: map[string]string{}}
	runner := newMockRunner(t)
	runner.EXPECT().Output(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, c process.Command) (process.Result, error) {
		require.Equal(t, "sudo", c.Name)
		call := strings.Join(c.Args, " ")
		s.calls = append(s.calls, call)
		if c.Stdin != nil {
			text, err := io.ReadAll(c.Stdin)
			require.NoError(t, err)
			s.written[c.Args[len(c.Args)-1]] = string(text)
		}
		if stderr, failed := failing[call]; failed {
			return process.Result{Exit: 1, Stderr: []byte(stderr)}, nil
		}
		return process.Result{}, nil
	}).Maybe()
	return runner, s
}

func present(t *testing.T, units ...Unit) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	for _, unit := range units {
		for _, file := range unit.Files() {
			text, err := Render(file, gorgon)
			require.NoError(t, err)
			files[dir+"/"+file] = []byte(text)
		}
	}
	return files
}

func reading(files map[string][]byte) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		if content, ok := files[path]; ok {
			return content, nil
		}
		return nil, os.ErrNotExist
	}
}

func TestNothingChangedWritesNothingAndStillEnablesTheTimers(t *testing.T) {
	runner, s := recording(t, nil)

	outcome, err := Installer{Runner: runner, Dir: dir, Read: reading(present(t, Update, Cleanup, Backup, Verify))}.Install(context.Background(), true, gorgon)

	require.NoError(t, err)
	assert.Equal(t, []string{"systemctl enable --now media-update.timer media-download-cleanup.timer media-backup.timer media-verify.timer"}, s.calls)
	assert.Equal(t, "all 4 unchanged", outcome.String())
}

func TestAChangedUnitIsWrittenAndSystemdReloaded(t *testing.T) {
	files := present(t, Update, Cleanup, Backup, Verify)
	files[dir+"/media-update.service"] = []byte("[Service]\nExecStart=/old/engine-run update\n")
	runner, s := recording(t, nil)

	outcome, err := Installer{Runner: runner, Dir: dir, Read: reading(files)}.Install(context.Background(), true, gorgon)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"tee /etc/systemd/system/media-update.service",
		"systemctl daemon-reload",
		"systemctl enable --now media-update.timer media-download-cleanup.timer media-backup.timer media-verify.timer",
	}, s.calls)
	want, err := Render("media-update.service", gorgon)
	require.NoError(t, err)
	assert.Equal(t, want, s.written["/etc/systemd/system/media-update.service"])
	assert.Equal(t, "media-update changed; media-download-cleanup, media-backup, media-verify unchanged", outcome.String())
}

func TestAFreshMachineGetsEveryFile(t *testing.T) {
	runner, s := recording(t, nil)

	outcome, err := Installer{Runner: runner, Dir: dir, Read: reading(nil)}.Install(context.Background(), false, gorgon)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"tee /etc/systemd/system/media-update.service", "tee /etc/systemd/system/media-update.timer",
		"tee /etc/systemd/system/media-download-cleanup.service", "tee /etc/systemd/system/media-download-cleanup.timer",
		"systemctl daemon-reload",
		"systemctl enable --now media-update.timer media-download-cleanup.timer",
	}, s.calls)
	assert.Equal(t, "media-update, media-download-cleanup changed", outcome.String())
}

func TestAFormerMainStopsBackingUp(t *testing.T) {
	runner, s := recording(t, nil)

	outcome, err := Installer{Runner: runner, Dir: dir, Read: reading(present(t, Update, Cleanup, Backup, Verify))}.Install(context.Background(), false, gorgon)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"systemctl disable --now media-backup.timer",
		"rm -f /etc/systemd/system/media-backup.service /etc/systemd/system/media-backup.timer",
		"systemctl disable --now media-verify.timer",
		"rm -f /etc/systemd/system/media-verify.service /etc/systemd/system/media-verify.timer",
		"systemctl daemon-reload",
		"systemctl enable --now media-update.timer media-download-cleanup.timer",
	}, s.calls)
	assert.Equal(t, "media-update, media-download-cleanup unchanged; media-backup, media-verify removed (not the main)", outcome.String())
}

func TestAFailedSudoStopsWithItsMessage(t *testing.T) {
	runner, s := recording(t, map[string]string{"tee /etc/systemd/system/media-update.service": "sudo: a password is required\n"})

	_, err := Installer{Runner: runner, Dir: dir, Read: reading(nil)}.Install(context.Background(), true, gorgon)

	assert.EqualError(t, err, "sudo tee /etc/systemd/system/media-update.service failed: sudo: a password is required")
	assert.Equal(t, []string{"tee /etc/systemd/system/media-update.service"}, s.calls)
}

func TestAFailureWithoutAMessageNamesTheExitCode(t *testing.T) {
	runner, _ := recording(t, map[string]string{"systemctl enable --now media-update.timer media-download-cleanup.timer media-backup.timer media-verify.timer": ""})

	_, err := Installer{Runner: runner, Dir: dir, Read: reading(present(t, Update, Cleanup, Backup, Verify))}.Install(context.Background(), true, gorgon)

	assert.EqualError(t, err, "sudo systemctl enable --now media-update.timer media-download-cleanup.timer media-backup.timer media-verify.timer failed: exit 1")
}
