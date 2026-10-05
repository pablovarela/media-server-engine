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
	deps        Dependencies
	written     map[string]string
	calls       []string
	mse         string
	snapshots   string
	noToken     bool
	tokenCall   []string
	environment map[string]string
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
	tf := &timersFixture{written: map[string]string{}, mse: resolved, snapshots: ourSnapshots}
	f.runner.EXPECT().Run(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, c process.Command) (int, error) {
		tf.calls = append(tf.calls, c.Name+" "+strings.Join(c.Args, " "))
		return 0, nil
	}).Maybe()
	f.runner.EXPECT().Output(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, c process.Command) (process.Result, error) {
		switch c.Name {
		case "restic":
			return process.Result{Stdout: []byte(tf.snapshots)}, nil
		case "env":
			tf.tokenCall = append([]string{c.Name}, c.Args...)
			if tf.noToken {
				return process.Result{Exit: 1, Stderr: []byte("no oauth token found for github.com\n")}, nil
			}
			return process.Result{Stdout: []byte("gho_not_shown\n")}, nil
		}
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
	machineID := filepath.Join(t.TempDir(), "machine-id")
	require.NoError(t, os.WriteFile(machineID, []byte("this-machine\n"), 0o644))
	tf.deps.MachineIDFile = machineID
	tf.environment = map[string]string{}
	tf.deps.Environment = func(key string) string { return tf.environment[key] }
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
	assert.Equal(t, "sudo -v", tf.calls[0], "sudo asks for the password before the step line")
	assert.Contains(t, tf.calls, "sudo systemctl daemon-reload")
	assert.Equal(t, []string{"env", "-i", "HOME=" + tf.deps.Home, "USER=pablo", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "gh", "auth", "token"}, tf.tokenCall)
}

func TestTheTimersNeedAGitHubTokenWithoutALogin(t *testing.T) {
	tf := newTimersFixture(t)
	tf.noToken = true

	code, _, stderr := tf.run(t)

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "the nightly mse update --apply can't get a GitHub token without a login: run gh auth login on this machine (gh keeps the token in ~/.config/gh/hosts.yml when there is no keyring); a GITHUB_TOKEN set in the shell doesn't reach the timers")
	assert.NotContains(t, stderr, "no oauth token")
	assert.Empty(t, tf.calls)
}

func TestAMachineAnotherOneBacksUpForGetsNoBackupTimers(t *testing.T) {
	tf := newTimersFixture(t)
	tf.snapshots = theirSnapshots

	code, stdout, stderr := tf.run(t)

	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "media-update, media-download-cleanup changed")
	assert.NotContains(t, strings.Join(tf.calls, "\n"), "media-backup")
	assert.NoFileExists(t, filepath.Join(tf.deps.Home, ".local", "share", "mse", "gorgon", ".backup-main"))
}

func TestTheMainIsToldByTheRepositoryNotOnlyByItsMarker(t *testing.T) {
	tf := newTimersFixture(t)
	marker := filepath.Join(tf.deps.Home, ".local", "share", "mse", "gorgon", ".backup-main")
	require.NoError(t, os.Remove(marker))

	code, stdout, stderr := tf.run(t)

	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "media-backup, media-verify changed")
	assert.FileExists(t, marker)
}

func TestTheInstallationsPathsAndKeyReachTheUnits(t *testing.T) {
	tf := newTimersFixture(t)
	tf.environment["SOPS_AGE_KEY_FILE"] = "/keys/age.txt"

	code, _, stderr := tf.run(t)

	require.Equal(t, 0, code, stderr)
	assert.Contains(t, tf.written[filepath.Join(tf.deps.UnitDir, "media-update.service")], "Environment=\"SOPS_AGE_KEY_FILE=/keys/age.txt\"\n")
	assert.Equal(t, []string{"env", "-i", "HOME=" + tf.deps.Home, "USER=pablo", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "SOPS_AGE_KEY_FILE=/keys/age.txt", "gh", "auth", "token"}, tf.tokenCall, "gh is asked in the units' environment")
}

func TestAKeyGivenByValueCantReachTheTimers(t *testing.T) {
	tf := newTimersFixture(t)
	tf.environment["SOPS_AGE_KEY"] = "AGE-SECRET-KEY-NOT-REAL"

	code, _, stderr := tf.run(t)

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "SOPS_AGE_KEY can't reach the timers without writing the key into a unit: keep the key in a file and set SOPS_AGE_KEY_FILE instead")
	assert.NotContains(t, stderr, "AGE-SECRET-KEY-NOT-REAL")
	assert.Empty(t, tf.calls)
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
