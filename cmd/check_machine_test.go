package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/machine"
	"github.com/pablovarela/media-server-engine/internal/paint"
	"github.com/pablovarela/media-server-engine/internal/process"
)

const machineCompose = "services:\n  jellyfin:\n    ports:\n      - \"8096:8096\"\n  homepage:\n    ports:\n      - \"${HOMEPAGE_PORT:-80}:3000\"\n"

type checkMachineFixture struct {
	deps    Dependencies
	answers map[string]process.Result
	effects map[string]func()
	checked []machine.Port
	runner  *mockCommandRunner
}

func newCheckMachineFixture(t *testing.T, installations map[string]string) *checkMachineFixture {
	t.Helper()
	_, home := xdgHome(t, installations)
	f := &checkMachineFixture{effects: map[string]func(){}, answers: map[string]process.Result{
		"git --version": {Stdout: []byte("git version 2.47.3\n")},
		"env -i HOME=" + home + " USER=pablo PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin gh auth token": {Stdout: []byte("gho_token\n")},
		"git config --global --get-all credential.https://github.com.helper":                                                  {Stdout: []byte("!/usr/bin/gh auth git-credential\n")},
		"docker --version":                   {Stdout: []byte("Docker version 29.1.0\n")},
		"id -Gn pablo":                       {Stdout: []byte("pablo docker\n")},
		"docker info":                        {Stdout: []byte("ok\n")},
		"loginctl show-user pablo -p Linger": {Stdout: []byte("Linger=yes\n")},
	}}
	runner := newMockCommandRunner(t)
	f.runner = runner
	runner.EXPECT().Output(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, c process.Command) (process.Result, error) {
		command := strings.Join(append([]string{c.Name}, c.Args...), " ")
		if effect := f.effects[command]; effect != nil {
			effect()
		}
		answer, known := f.answers[command]
		if !known {
			return process.Result{Exit: 1}, nil
		}
		return answer, nil
	}).Maybe()
	f.deps = Dependencies{
		ResticBinary: localRestic,
		Environment:  func(string) string { return "" },
		Home:         home,
		Engine:       fstest.MapFS{"docker-compose.yml": {Data: []byte(machineCompose)}},
		Run:          func(_, _ io.Writer) commandRunner { return runner },
		Account:      func() (string, error) { return "pablo", nil },
		GOOS:         "linux",
		Systemd:      func() bool { return true },
		ProcRoot:     t.TempDir(),
		PortFree: func(p machine.Port) bool {
			f.checked = append(f.checked, p)
			return true
		},
		Published: func(context.Context) ([]machine.Published, error) { return nil, nil },
	}
	return f
}

func (f *checkMachineFixture) check(t *testing.T) (int, string, string) {
	t.Helper()
	root := NewRootCommand(f.deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	code := run(context.Background(), root, []string{"check-machine"})
	return code, stdout.String(), stderr.String()
}

func TestCheckMachineOnAReadyMachine(t *testing.T) {
	f := newCheckMachineFixture(t, nil)

	code, stdout, stderr := f.check(t)

	assert.Equal(t, 0, code)
	assert.Empty(t, stderr)
	assert.Equal(t, "Checking this machine...\n"+
		"  git... ✓\n  gh is logged in... ✓ token on disk\n  git uses gh for github.com... ✓\n  Docker... ✓\n  pablo is in the docker group... ✓\n"+
		"  Docker answers... ✓\n  lingering... ✓\n  the user manager has the docker group... ✓\n  ports... ✓ 8096, 80 free\n"+
		"\nThis machine is ready.\n", stdout)
}

func TestCheckMachineWithSomethingMissing(t *testing.T) {
	f := newCheckMachineFixture(t, nil)
	delete(f.answers, "loginctl show-user pablo -p Linger")

	code, stdout, stderr := f.check(t)

	assert.Equal(t, 1, code)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "  lingering... ✗ off\n      run: sudo loginctl enable-linger pablo\n")
	assert.True(t, strings.HasSuffix(stdout, "\n1 thing to fix. Run mse check-machine again afterwards.\n"))
}

func TestCheckMachineUsesTheInstallationsHomepagePort(t *testing.T) {
	f := newCheckMachineFixture(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\nHOMEPAGE_PORT=8080\n"})

	code, stdout, _ := f.check(t)

	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "  ports... ✓ 8096, 8080 free\n")
	assert.Contains(t, f.checked, machine.Port{Number: 8080, Protocol: "tcp"})
}

func TestCheckMachineRefusesSeveralInstallations(t *testing.T) {
	f := newCheckMachineFixture(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\nHOMEPAGE_PORT=8080\n", "medusa": "INSTALLATION_NAME=medusa\n"})

	code, stdout, stderr := f.check(t)

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "a machine runs one installation; remove the ones it shouldn't have:\n  gorgon\n  medusa")
}

func TestCheckMachineLooksForTheTokenWhereTheTimersWould(t *testing.T) {
	f := newCheckMachineFixture(t, nil)
	cfg := f.deps.Home + "/cfg"
	f.deps.Environment = func(name string) string { return map[string]string{"XDG_CONFIG_HOME": cfg}[name] }
	delete(f.answers, "env -i HOME="+f.deps.Home+" USER=pablo PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin gh auth token")
	f.answers["env -i HOME="+f.deps.Home+" USER=pablo PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin XDG_CONFIG_HOME="+cfg+" gh auth token"] = process.Result{Stdout: []byte("gho_token\n")}

	_, stdout, _ := f.check(t)

	assert.Contains(t, stdout, "  gh is logged in... ✓ token on disk\n")
}

func TestCheckMachineChecksTheOverridesPortsToo(t *testing.T) {
	f := newCheckMachineFixture(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
	override := "services:\n  homepage:\n    ports:\n      - \"8443:443\"\n  jellyfin:\n    ports:\n      - \"8096:8096\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(f.deps.Home, ".config", "mse", "gorgon", "compose.override.yml"), []byte(override), 0o644))

	_, stdout, _ := f.check(t)

	assert.Contains(t, stdout, "  ports... ✓ 8096, 80, 8443 free\n")
}

func TestCheckMachineIsNotReadyWhenSomethingCouldNotBeChecked(t *testing.T) {
	f := newCheckMachineFixture(t, nil)
	f.deps.PortFree = func(machine.Port) bool { return false }
	f.deps.Published = func(context.Context) ([]machine.Published, error) { return nil, errors.New("permission denied") }

	code, stdout, stderr := f.check(t)

	assert.Equal(t, 1, code)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "  ports... ? 8096, 80 in use, and Docker couldn't say by what (permission denied)\n")
}

func TestTheChecksDrawADotForEachSecondTheyTake(t *testing.T) {
	var out bytes.Buffer
	ticks := make(chan time.Time)
	stopped := false
	printer := &printedChecks{out: &out, painter: &paint.Painter{}, ticker: func() (<-chan time.Time, func()) {
		return ticks, func() { stopped = true }
	}}

	printer.Started("Docker answers")
	for range 3 {
		ticks <- time.Now()
	}
	printer.Finished(machine.Result{Name: "Docker answers", Status: machine.Pass})
	printer.Started("lingering")
	printer.Finished(machine.Result{Name: "lingering", Status: machine.Fail, Detail: "off", Fix: "sudo loginctl enable-linger pablo"})

	assert.Equal(t, "  Docker answers...... ✓\n  lingering... ✗ off\n      run: sudo loginctl enable-linger pablo\n", out.String())
	assert.True(t, stopped)
}

func TestWithoutATerminalThereAreNoDots(t *testing.T) {
	var out bytes.Buffer
	printer := &printedChecks{out: &out, painter: &paint.Painter{}}

	printer.Started("git")
	printer.Finished(machine.Result{Name: "git", Status: machine.Pass})

	assert.Equal(t, "  git... ✓\n", out.String())
}
