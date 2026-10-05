package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/process"
	"github.com/pablovarela/media-server-engine/internal/version"
)

type timersFixture struct {
	deps    Dependencies
	written map[string]string
	calls   []string
	mse     string
}

func newTimersFixture(t *testing.T) *timersFixture {
	t.Helper()
	f := newApplyFixture(t)
	installed := filepath.Join(t.TempDir(), "opt", "mse")
	require.NoError(t, os.MkdirAll(filepath.Dir(installed), 0o755))
	require.NoError(t, os.WriteFile(installed, nil, 0o755))
	link := filepath.Join(t.TempDir(), "mse")
	require.NoError(t, os.Symlink(installed, link))
	resolved, err := filepath.EvalSymlinks(installed)
	require.NoError(t, err)
	tf := &timersFixture{written: map[string]string{}, mse: resolved}
	f.runner.EXPECT().Output(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, c process.Command) (process.Result, error) {
		tf.calls = append(tf.calls, c.Name+" "+strings.Join(c.Args, " "))
		if c.Stdin != nil {
			text, err := io.ReadAll(c.Stdin)
			require.NoError(t, err)
			tf.written[c.Args[len(c.Args)-1]] = string(text)
		}
		return process.Result{}, nil
	}).Maybe()
	tf.deps = f.deps(t, true)
	tf.deps.Build = version.Build{Version: "v0.14.0"}
	tf.deps.Executable = func() (string, error) { return link, nil }
	tf.deps.UnitDir = t.TempDir()
	tf.deps.Account = func() (string, string, error) { return "pablo", "pablo", nil }
	return tf
}

func (tf *timersFixture) run(t *testing.T) (int, string, string) {
	t.Helper()
	root := NewRootCommand(tf.deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	code := run(context.Background(), root, []string{"install-timers"})
	return code, stdout.String(), stderr.String()
}

func TestInstallTimersOnTheMain(t *testing.T) {
	tf := newTimersFixture(t)

	code, stdout, stderr := tf.run(t)

	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "Installing the timers... media-update, media-download-cleanup, media-backup, media-verify changed.\n", stdout)
	update := tf.written[filepath.Join(tf.deps.UnitDir, "media-update.service")]
	assert.Contains(t, update, "ExecStart="+tf.mse+" update --apply\n")
	assert.Contains(t, update, "Environment=MSE_INSTALLATION=gorgon\n")
	assert.Contains(t, update, "User=pablo\n")
	assert.Contains(t, tf.calls, "sudo systemctl daemon-reload")
}

func TestInstallTimersNeedsSystemd(t *testing.T) {
	tf := newTimersFixture(t)
	tf.deps.Systemd = func() bool { return false }

	code, _, stderr := tf.run(t)

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "timers need systemd, and systemd is not running on this machine")
	assert.Empty(t, tf.calls)
}

func TestInstallTimersRefusesADevBuild(t *testing.T) {
	tf := newTimersFixture(t)
	tf.deps.Build = version.Build{Version: "dev"}

	code, _, stderr := tf.run(t)

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "a dev build can't run timers: mse update can't replace it; install a release with install.sh")
	assert.Empty(t, tf.calls)
}
