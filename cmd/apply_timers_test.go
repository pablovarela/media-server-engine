package cmd

import (
	"bytes"
	"context"
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
	f           applyFixture
	deps        Dependencies
	units       string
	mse         string
	snapshots   process.Result
	linger      string
	token       string
	environment map[string]string
	systemctl   []string
	tokenCall   []string
}

func newTimersFixture(t *testing.T) *timersFixture {
	t.Helper()
	tf := &timersFixture{
		f: newApplyFixture(t), snapshots: process.Result{Stdout: []byte(ourSnapshots)},
		linger: "Linger=yes\n", token: "gho_not_shown\n", environment: map[string]string{},
	}
	installed := filepath.Join(t.TempDir(), "opt", "mse")
	require.NoError(t, os.MkdirAll(filepath.Dir(installed), 0o755))
	require.NoError(t, os.WriteFile(installed, nil, 0o755))
	resolved, err := filepath.EvalSymlinks(installed)
	require.NoError(t, err)
	tf.mse = resolved
	link := filepath.Join(t.TempDir(), "mse")
	require.NoError(t, os.Symlink(installed, link))
	tf.f.runner.EXPECT().Output(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, c process.Command) (process.Result, error) {
		switch c.Name {
		case "timedatectl":
			return process.Result{Stdout: []byte("Europe/London\n")}, nil
		case "loginctl":
			assert.Equal(t, []string{"show-user", "pablo", "-p", "Linger"}, c.Args)
			return process.Result{Stdout: []byte(tf.linger)}, nil
		case "env":
			tf.tokenCall = append([]string{c.Name}, c.Args...)
			return process.Result{Stdout: []byte(tf.token)}, nil
		case "restic":
			return tf.snapshots, nil
		case "systemctl":
			tf.systemctl = append(tf.systemctl, strings.Join(c.Args, " "))
			return process.Result{}, nil
		}
		t.Fatalf("unexpected command %s %v", c.Name, c.Args)
		return process.Result{}, nil
	}).Maybe()
	tf.f.expectApply(nil)
	tf.deps = tf.f.deps(t, true)
	tf.deps.Build = version.Build{Version: "v0.14.0"}
	tf.deps.Executable = func() (string, error) { return link, nil }
	tf.units = filepath.Join(t.TempDir(), "systemd", "user")
	tf.deps.UnitDir = tf.units
	tf.deps.Account = func() (string, string, error) { return "pablo", "pablo", nil }
	machineID := filepath.Join(t.TempDir(), "machine-id")
	require.NoError(t, os.WriteFile(machineID, []byte("this-machine\n"), 0o644))
	tf.deps.MachineIDFile = machineID
	tf.deps.Environment = func(key string) string { return tf.environment[key] }
	return tf
}

func (tf *timersFixture) apply(t *testing.T) (int, string, string) {
	t.Helper()
	root := NewRootCommand(tf.deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	code := run(context.Background(), root, []string{"apply"})
	return code, stdout.String(), stderr.String()
}

func (tf *timersFixture) unit(t *testing.T, file string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(tf.units, file))
	require.NoError(t, err)
	return string(content)
}

func (tf *timersFixture) placeBackupUnits(t *testing.T) {
	t.Helper()
	require.NoError(t, os.MkdirAll(tf.units, 0o755))
	for _, file := range []string{"mse-gorgon-backup.service", "mse-gorgon-backup.timer", "mse-gorgon-verify.service", "mse-gorgon-verify.timer"} {
		require.NoError(t, os.WriteFile(filepath.Join(tf.units, file), []byte("placed before\n"), 0o644))
	}
}

func TestApplySetsUpTheMainsTimers(t *testing.T) {
	tf := newTimersFixture(t)

	code, stdout, stderr := tf.apply(t)

	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "Setting up the timers... mse-gorgon-update, mse-gorgon-download-cleanup, mse-gorgon-backup, mse-gorgon-verify changed.\n")
	update := tf.unit(t, "mse-gorgon-update.service")
	assert.Contains(t, update, "ExecStart="+tf.mse+" update --apply\n")
	assert.Contains(t, update, "Environment=MSE_INSTALLATION=gorgon\n")
	assert.Contains(t, tf.systemctl, "--user enable --now mse-gorgon-update.timer mse-gorgon-download-cleanup.timer mse-gorgon-backup.timer mse-gorgon-verify.timer")
	assert.Equal(t, []string{"env", "-i", "HOME=" + tf.deps.Home, "USER=pablo", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "gh", "auth", "token"}, tf.tokenCall)
}

func TestAMachineAnotherOneBacksUpForLosesItsBackupTimers(t *testing.T) {
	tf := newTimersFixture(t)
	tf.snapshots = process.Result{Stdout: []byte(theirSnapshots)}
	tf.placeBackupUnits(t)

	code, stdout, stderr := tf.apply(t)

	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "mse-gorgon-backup, mse-gorgon-verify removed (not the main)")
	assert.NoFileExists(t, filepath.Join(tf.units, "mse-gorgon-backup.timer"))
	assert.NoFileExists(t, filepath.Join(tf.f.data, ".backup-main"))
}

func TestWithoutLingeringNoUnitIsWritten(t *testing.T) {
	tf := newTimersFixture(t)
	tf.linger = "Linger=no\n"

	code, stdout, stderr := tf.apply(t)

	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "Setting up the timers... skipped: they need lingering, once: sudo loginctl enable-linger pablo.\n")
	assert.NoDirExists(t, tf.units)
	assert.Empty(t, tf.systemctl)
}

func TestADevBuildSetsUpNoTimers(t *testing.T) {
	tf := newTimersFixture(t)
	tf.deps.Build = version.Build{Version: "dev"}

	code, stdout, stderr := tf.apply(t)

	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "Setting up the timers... skipped: a dev build can't be updated by mse update.\n")
	assert.NoDirExists(t, tf.units)
}

func TestAKeyGivenByValueSetsUpNoTimers(t *testing.T) {
	tf := newTimersFixture(t)
	tf.environment["SOPS_AGE_KEY"] = "AGE-SECRET-KEY-NOT-REAL"

	code, stdout, stderr := tf.apply(t)

	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "Setting up the timers... skipped: SOPS_AGE_KEY can't reach the timers; keep the key in a file and set SOPS_AGE_KEY_FILE.\n")
	assert.NotContains(t, stdout+stderr, "AGE-SECRET-KEY-NOT-REAL")
	assert.NoDirExists(t, tf.units)
}

func TestTheCarriedVariablesReachTheUnitsAndTheTokenCheck(t *testing.T) {
	tf := newTimersFixture(t)
	tf.environment["SOPS_AGE_KEY_FILE"] = "/keys/age.txt"

	code, _, stderr := tf.apply(t)

	require.Equal(t, 0, code, stderr)
	assert.Contains(t, tf.unit(t, "mse-gorgon-backup.service"), "Environment=\"SOPS_AGE_KEY_FILE=/keys/age.txt\"\n")
	assert.Contains(t, tf.tokenCall, "SOPS_AGE_KEY_FILE=/keys/age.txt")
}

func TestWithoutAnUnattendedTokenTheTimersAreStillSetUpWithAWarning(t *testing.T) {
	tf := newTimersFixture(t)
	tf.token = ""

	code, stdout, stderr := tf.apply(t)

	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "the nightly update can't get a GitHub token without a login: run gh auth login (gh keeps the token in ~/.config/gh/hosts.yml when there is no keyring); a GITHUB_TOKEN in the shell doesn't reach the timers")
	assert.Contains(t, stdout, "mse-gorgon-update")
	assert.FileExists(t, filepath.Join(tf.units, "mse-gorgon-update.timer"))
}

func TestAnUnreadableRepositoryLeavesTheBackupTimersAsTheyAre(t *testing.T) {
	tf := newTimersFixture(t)
	tf.snapshots = process.Result{Exit: 1, Stderr: []byte("Fatal: unable to open repository\n")}
	tf.placeBackupUnits(t)

	code, stdout, stderr := tf.apply(t)

	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "could not tell whether this machine is the main")
	assert.Contains(t, stderr, "the backup timers are left as they are")
	assert.Contains(t, stdout, "mse-gorgon-update, mse-gorgon-download-cleanup changed.\n")
	assert.Equal(t, "placed before\n", tf.unit(t, "mse-gorgon-backup.timer"))
}

func TestWithoutSystemdThereIsNoTimersStep(t *testing.T) {
	tf := newTimersFixture(t)
	tf.deps.Systemd = func() bool { return false }

	code, stdout, stderr := tf.apply(t)

	require.Equal(t, 0, code, stderr)
	assert.NotContains(t, stdout, "Setting up the timers")
}

func TestInstallTimersIsGone(t *testing.T) {
	root := NewRootCommand(Dependencies{Home: t.TempDir(), Environment: func(string) string { return "" }})
	var stderr bytes.Buffer
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"install-timers"})

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), `unknown command "install-timers"`)
}
