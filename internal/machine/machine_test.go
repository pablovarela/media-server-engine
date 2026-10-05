package machine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/paint"
	"github.com/pablovarela/media-server-engine/internal/process"
)

const bareToken = "env -i HOME=/home/pablo USER=pablo PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin gh auth token"

type fixture struct {
	env     Env
	answers map[string]process.Result
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{answers: map[string]process.Result{
		"git --version": {Stdout: []byte("git version 2.47.3\n")},
		bareToken:       {Stdout: []byte("gho_token\n")},
		"git config --global --get-all credential.https://github.com.helper": {Stdout: []byte("\n!/usr/bin/gh auth git-credential\n")},
		"docker --version":                   {Stdout: []byte("Docker version 29.1.0\n")},
		"id -Gn pablo":                       {Stdout: []byte("pablo adm docker\n")},
		"docker info":                        {Stdout: []byte("Server Version: 29.1.0\n")},
		"restic version":                     {Stdout: []byte("restic 0.18.1\n")},
		"loginctl show-user pablo -p Linger": {Stdout: []byte("Linger=yes\n")},
		"getent group docker":                {Stdout: []byte("docker:x:995:pablo\n")},
		"id -u pablo":                        {Stdout: []byte("1000\n")},
		"systemctl show user@1000.service -p MainPID --value": {Stdout: []byte("4242\n")},
	}}
	procRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(procRoot, "4242"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(procRoot, "4242", "status"), []byte("Name:\tsystemd\nGroups:\t4 995 1000\n"), 0o644))
	runner := newMockRunner(t)
	runner.EXPECT().Output(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, c process.Command) (process.Result, error) {
		call := strings.Join(append([]string{c.Name}, c.Args...), " ")
		answer, known := f.answers[call]
		if !known {
			return process.Result{Exit: 127, Stderr: []byte(c.Name + ": not found\n")}, nil
		}
		return answer, nil
	}).Maybe()
	f.env = Env{Runner: runner, Account: "pablo", Home: "/home/pablo", GOOS: "linux", Systemd: true, ProcRoot: procRoot}
	return f
}

func (f *fixture) fails(call string) {
	f.answers[call] = process.Result{Exit: 1}
}

type written struct{ out *strings.Builder }

func (w written) Started(name string)    { w.out.WriteString(Begin(name)) }
func (w written) Finished(result Result) { w.out.WriteString(End(result, &paint.Painter{})) }

func (f *fixture) render(t *testing.T) string {
	t.Helper()
	var out strings.Builder
	report := RunEach(context.Background(), f.env, written{&out})
	out.WriteString(report.Closing())
	return out.String()
}

const readyLinux = `  git... ✓
  gh is logged in... ✓ token on disk
  git uses gh for github.com... ✓
  Docker... ✓
  pablo is in the docker group... ✓
  Docker answers... ✓
  restic... ✓
  lingering... ✓
  the user manager has the docker group... ✓

This machine is ready.
`

func TestAReadyMachine(t *testing.T) {
	f := newFixture(t)

	assert.Equal(t, 0, Run(context.Background(), f.env).Problems())
	assert.Equal(t, readyLinux, f.render(t))
}

func TestEachMissingPieceSaysHowToFixIt(t *testing.T) {
	tests := map[string]struct {
		call, line string
	}{
		"git":       {"git --version", "  git... ✗ not installed\n      run: sudo apt install git\n"},
		"helper":    {"git config --global --get-all credential.https://github.com.helper", "  git uses gh for github.com... ✗\n      run: gh auth setup-git\n"},
		"docker":    {"docker --version", "  Docker... ✗ not installed\n      run: curl -fsSL https://get.docker.com | sudo sh\n"},
		"session":   {"docker info", "  Docker answers... ✗ this session doesn't have the docker group yet\n      run: log out and back in\n"},
		"restic":    {"restic version", "  restic... ✗ not installed\n      run: sudo apt install restic\n"},
		"lingering": {"loginctl show-user pablo -p Linger", "  lingering... ✗ off\n      run: sudo loginctl enable-linger pablo\n"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.fails(tt.call)

			out := f.render(t)

			assert.Contains(t, out, tt.line)
			assert.True(t, strings.HasSuffix(out, "\n1 thing to fix. Run mse check-machine again afterwards.\n"), out)
		})
	}
}

func TestGhNotLoggedIn(t *testing.T) {
	f := newFixture(t)
	f.fails(bareToken)
	f.fails("gh auth token")

	out := f.render(t)

	assert.Contains(t, out, "  gh is logged in... ✗ not logged in\n      run: gh auth login\n")
	assert.Contains(t, out, "  git uses gh for github.com... ✓\n")
}

func TestGhOffTheTimersPath(t *testing.T) {
	f := newFixture(t)
	f.answers[bareToken] = process.Result{Exit: 127, Stderr: []byte("env: 'gh': No such file or directory\n")}
	f.answers["gh --version"] = process.Result{Stdout: []byte("gh version 2.83.0\n")}

	assert.Contains(t, f.render(t), "  gh is logged in... ✗ gh isn't on the timers' PATH (/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin)\n      run: sudo ln -s \"$(command -v gh)\" /usr/local/bin/gh\n")
}

func TestGhNotInstalled(t *testing.T) {
	f := newFixture(t)
	f.answers[bareToken] = process.Result{Exit: 127}

	assert.Contains(t, f.render(t), "  gh is logged in... ✗ gh isn't installed\n      run: sudo apt install gh\n")
}

func TestATokenFromTheEnvironmentIsCaught(t *testing.T) {
	f := newFixture(t)
	f.fails(bareToken)
	f.answers["gh auth token"] = process.Result{Stdout: []byte("ghp_from_env\n")}
	f.env.Getenv = func(name string) string { return map[string]string{"GITHUB_TOKEN": "ghp_from_env"}[name] }

	assert.Contains(t, f.render(t), "  gh is logged in... ✗ its token comes from GITHUB_TOKEN, which the timers can't read\n      run: unset GITHUB_TOKEN, then gh auth login --insecure-storage\n")
}

func TestTheTokenProbeCarriesTheUnitsVariables(t *testing.T) {
	f := newFixture(t)
	f.env.Carried = []string{"XDG_CONFIG_HOME=/home/pablo/cfg"}
	f.fails(bareToken)
	f.answers["env -i HOME=/home/pablo USER=pablo PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin XDG_CONFIG_HOME=/home/pablo/cfg gh auth token"] = process.Result{Stdout: []byte("gho_token\n")}

	assert.Contains(t, f.render(t), "  gh is logged in... ✓ token on disk\n")
}

func TestATokenOnlyInAKeyringIsCaught(t *testing.T) {
	f := newFixture(t)
	f.fails(bareToken)
	f.answers["gh auth token"] = process.Result{Stdout: []byte("gho_token\n")}

	out := f.render(t)

	assert.Contains(t, out, "  gh is logged in... ✗ its token is only in a keyring, which the timers can't read\n      run: gh auth login --insecure-storage\n")
}

func TestNotInTheDockerGroup(t *testing.T) {
	f := newFixture(t)
	f.answers["id -Gn pablo"] = process.Result{Stdout: []byte("pablo adm\n")}

	out := f.render(t)

	assert.Contains(t, out, "  pablo is in the docker group... ✗\n      run: sudo usermod -aG docker pablo, then log out and back in\n")
	assert.Contains(t, out, "  Docker answers... – skipped until pablo is in the docker group\n")
	assert.Contains(t, out, "  the user manager has the docker group... – skipped until pablo is in the docker group\n")
	assert.Contains(t, out, "1 thing to fix.")
}

func TestASessionWithoutTheNewGroupStillChecksTheManager(t *testing.T) {
	f := newFixture(t)
	f.fails("docker info")

	out := f.render(t)

	assert.Contains(t, out, "  pablo is in the docker group... ✓\n")
	assert.Contains(t, out, "  Docker answers... ✗ this session doesn't have the docker group yet\n")
	assert.Contains(t, out, "  the user manager has the docker group... ✓\n")
}

func TestNoDockerSkipsWhatNeedsIt(t *testing.T) {
	f := newFixture(t)
	f.fails("docker --version")

	out := f.render(t)

	assert.Contains(t, out, "  pablo is in the docker group... – skipped until Docker is installed\n")
	assert.Contains(t, out, "  Docker answers... – skipped until Docker is installed\n")
	assert.Contains(t, out, "  the user manager has the docker group... – skipped until Docker is installed\n")
	assert.Contains(t, out, "1 thing to fix.")
}

func TestAStaleUserManager(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(f.env.ProcRoot, "4242", "status"), []byte("Groups:\t4 1000\n"), 0o644))

	out := f.render(t)

	assert.Contains(t, out, "  the user manager has the docker group... ✗ it started before pablo joined the docker group\n      run: sudo systemctl restart user@1000 (or reboot)\n")
}

func TestTwoProblems(t *testing.T) {
	f := newFixture(t)
	f.fails("restic version")
	f.fails("loginctl show-user pablo -p Linger")

	assert.True(t, strings.HasSuffix(f.render(t), "\n2 things to fix. Run mse check-machine again afterwards.\n"))
}

func TestAMac(t *testing.T) {
	f := newFixture(t)
	f.env.GOOS = "darwin"
	f.env.Systemd = false
	delete(f.answers, bareToken)
	f.answers["gh auth token"] = process.Result{Stdout: []byte("gho_keychain\n")}
	f.fails("git --version")
	f.fails("docker info")
	f.fails("restic version")

	out := f.render(t)

	assert.Equal(t, `  git... ✗ not installed
      run: xcode-select --install
  gh is logged in... ✓
  git uses gh for github.com... – skipped until git is installed
  Docker... ✓
  Docker answers... ✗
      run: start Docker
  restic... ✗ not installed
      run: brew install restic
  timers... – no systemd here, so nothing runs unattended; run mse update --apply yourself

3 things to fix. Run mse check-machine again afterwards.
`, out)
}

func TestAMacWithoutGh(t *testing.T) {
	f := newFixture(t)
	f.env.GOOS = "darwin"
	f.env.Systemd = false

	assert.Contains(t, f.render(t), "  gh is logged in... ✗ gh isn't installed\n      run: brew install gh\n")
}

func TestAStoppedDockerWhenTheSessionHasTheGroup(t *testing.T) {
	f := newFixture(t)
	f.fails("docker info")
	f.answers["id -Gn"] = process.Result{Stdout: []byte("pablo adm docker\n")}

	assert.Contains(t, f.render(t), "  Docker answers... ✗ the daemon doesn't answer\n      run: sudo systemctl start docker\n")
}

func stalling(t *testing.T, f *fixture, call string) {
	t.Helper()
	answering := f.env.Runner
	runner := newMockRunner(t)
	runner.EXPECT().Output(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, c process.Command) (process.Result, error) {
		if strings.Join(append([]string{c.Name}, c.Args...), " ") == call {
			<-ctx.Done()
			return process.Result{Exit: -1}, errors.New(c.Name + " was interrupted")
		}
		return answering.Output(ctx, c)
	}).Maybe()
	f.env.Runner = runner
	f.env.Timeout = 20 * time.Millisecond
}

func TestAStalledDockerDoesNotHangTheChecks(t *testing.T) {
	f := newFixture(t)
	stalling(t, f, "docker info")

	out := f.render(t)

	assert.Contains(t, out, "  Docker answers... ? no answer within 20ms\n")
	assert.True(t, strings.HasSuffix(out, "\n1 thing couldn't be checked; the lines above say why. Run mse check-machine again.\n"), out)
}

func TestACheckThatRunsOutOfTimeIsNotAVerdict(t *testing.T) {
	f := newFixture(t)
	stalling(t, f, "loginctl show-user pablo -p Linger")
	f.fails("restic version")

	out := f.render(t)

	assert.Contains(t, out, "  lingering... ? no answer within 20ms\n")
	assert.NotContains(t, out, "lingering... ✗")
	assert.Contains(t, out, "  the user manager has the docker group... – skipped until lingering is on\n")
	assert.True(t, strings.HasSuffix(out, "\n1 thing to fix, and 1 couldn't be checked. Run mse check-machine again afterwards.\n"), out)
}

type event struct {
	started string
	result  *Result
}

type recorded struct{ events *[]event }

func (r recorded) Started(name string)    { *r.events = append(*r.events, event{started: name}) }
func (r recorded) Finished(result Result) { *r.events = append(*r.events, event{result: &result}) }

func TestEachCheckStartsBeforeItsProbeAndFinishesBeforeTheNext(t *testing.T) {
	f := newFixture(t)
	answering := f.env.Runner
	var events []event
	runner := newMockRunner(t)
	runner.EXPECT().Output(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, c process.Command) (process.Result, error) {
		if c.Name == "restic" {
			require.Len(t, events, 13, "six checks started and finished, then restic started")
			assert.Equal(t, "restic", events[12].started)
			assert.Equal(t, "Docker answers", events[11].result.Name)
		}
		return answering.Output(ctx, c)
	}).Maybe()
	f.env.Runner = runner

	report := RunEach(context.Background(), f.env, recorded{&events})

	assert.True(t, report.Ready())
	assert.Len(t, events, 18)
}
