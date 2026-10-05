package cmd

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/pablovarela/media-server-engine/internal/machine"
	"github.com/pablovarela/media-server-engine/internal/process"
)

const machineCompose = "services:\n  jellyfin:\n    ports:\n      - \"8096:8096\"\n  homepage:\n    ports:\n      - \"${HOMEPAGE_PORT:-80}:3000\"\n"

type checkMachineFixture struct {
	deps    Dependencies
	answers map[string]process.Result
	checked []machine.Port
}

func newCheckMachineFixture(t *testing.T, installations map[string]string) *checkMachineFixture {
	t.Helper()
	_, home := xdgHome(t, installations)
	f := &checkMachineFixture{answers: map[string]process.Result{
		"git --version": {Stdout: []byte("git version 2.47.3\n")},
		"env -i HOME=" + home + " USER=pablo PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin gh auth token": {Stdout: []byte("gho_token\n")},
		"git config --global --get-all credential.https://github.com.helper":                                                  {Stdout: []byte("!/usr/bin/gh auth git-credential\n")},
		"docker --version":                   {Stdout: []byte("Docker version 29.1.0\n")},
		"id -Gn pablo":                       {Stdout: []byte("pablo docker\n")},
		"docker info":                        {Stdout: []byte("ok\n")},
		"restic version":                     {Stdout: []byte("restic 0.18.1\n")},
		"loginctl show-user pablo -p Linger": {Stdout: []byte("Linger=yes\n")},
	}}
	runner := newMockCommandRunner(t)
	runner.EXPECT().Output(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, c process.Command) (process.Result, error) {
		answer, known := f.answers[strings.Join(append([]string{c.Name}, c.Args...), " ")]
		if !known {
			return process.Result{Exit: 1}, nil
		}
		return answer, nil
	}).Maybe()
	f.deps = Dependencies{
		Environment: func(string) string { return "" },
		Home:        home,
		Engine:      fstest.MapFS{"docker-compose.yml": {Data: []byte(machineCompose)}},
		Run:         func(_, _ io.Writer) commandRunner { return runner },
		Account:     func() (string, error) { return "pablo", nil },
		GOOS:        "linux",
		Systemd:     func() bool { return true },
		ProcRoot:    t.TempDir(),
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
		"  ✓ git\n  ✓ gh is logged in (token on disk)\n  ✓ git uses gh for github.com\n  ✓ Docker\n  ✓ pablo is in the docker group\n"+
		"  ✓ Docker answers\n  ✓ restic\n  ✓ lingering\n  ✓ the user manager has the docker group\n  ✓ ports 8096, 80 free\n"+
		"\nThis machine is ready.\n", stdout)
}

func TestCheckMachineWithSomethingMissing(t *testing.T) {
	f := newCheckMachineFixture(t, nil)
	delete(f.answers, "restic version")

	code, stdout, stderr := f.check(t)

	assert.Equal(t, 1, code)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "  ✗ restic isn't installed\n      run: sudo apt install restic\n")
	assert.True(t, strings.HasSuffix(stdout, "\n1 thing to fix. Run mse check-machine again afterwards.\n"))
}

func TestCheckMachineUsesTheInstallationsHomepagePort(t *testing.T) {
	f := newCheckMachineFixture(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\nHOMEPAGE_PORT=8080\n"})

	code, stdout, _ := f.check(t)

	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "  ✓ ports 8096, 8080 free\n")
	assert.Contains(t, f.checked, machine.Port{Number: 8080, Protocol: "tcp"})
}

func TestCheckMachineWithSeveralInstallations(t *testing.T) {
	f := newCheckMachineFixture(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\nHOMEPAGE_PORT=8080\n", "medusa": "INSTALLATION_NAME=medusa\n"})

	_, stdout, _ := f.check(t)

	assert.Contains(t, stdout, "  ✓ ports 8096, 80 free (several installations here; using the default homepage port 80)\n")
}
