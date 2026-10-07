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

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/configure"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/machine"
	"github.com/pablovarela/media-server-engine/internal/process"
	"github.com/pablovarela/media-server-engine/internal/secrets"
	"github.com/pablovarela/media-server-engine/internal/version"
)

type joinFixture struct {
	*checkMachineFixture
	repositories *mockConfigRepositories
	prompter     *mockPrompter
	config, keys string
	secret       string
}

const joinCloneURL = "https://github.com/pablovarela/media-server-config-gorgon.git"

func newJoinFixture(t *testing.T) *joinFixture {
	t.Helper()
	m := newCheckMachineFixture(t, nil)
	for _, variable := range []string{"SOPS_AGE_KEY", "SOPS_AGE_KEY_CMD", "SOPS_AGE_KEY_FILE"} {
		t.Setenv(variable, "")
		require.NoError(t, os.Unsetenv(variable))
	}
	keyText, err := os.ReadFile("../internal/secrets/testdata/age.key")
	require.NoError(t, err)
	f := &joinFixture{
		checkMachineFixture: m,
		repositories:        newMockConfigRepositories(t),
		prompter:            newMockPrompter(t),
		config:              filepath.Join(m.deps.Home, ".config", "mse", "gorgon"),
		keys:                filepath.Join(m.deps.Home, ".config", "sops", "age", "keys.txt"),
	}
	for _, line := range strings.Split(string(keyText), "\n") {
		if strings.HasPrefix(line, "AGE-SECRET-KEY-") {
			f.secret = line
		}
	}
	m.effects["git clone --quiet -- "+joinCloneURL+" "+f.config] = func() { writeClonedConfig(t, f.config) }
	m.answers["git clone --quiet -- "+joinCloneURL+" "+f.config] = process.Result{}
	engine := m.deps.Engine.(fstest.MapFS)
	engine["docker-compose.monitoring.yml"] = &fstest.MapFile{Data: []byte("services: {}\n")}
	engine["grafana/grafana.ini"] = &fstest.MapFile{Data: []byte("\n")}
	engine["prometheus/prometheus.yml"] = &fstest.MapFile{Data: []byte("\n")}
	engine["scripts/backup-excludes.txt"] = &fstest.MapFile{Data: []byte("logs\n")}
	m.deps.Repositories = f.repositories
	m.deps.Prompter = func(context.Context) prompter { return f.prompter }
	m.deps.Decrypt = secrets.Sops(filepath.Join(m.deps.Home, ".config"))
	m.deps.Terminal = func() bool { return true }
	m.deps.Now = func() time.Time { return time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC) }
	m.deps.Host = installation.Host{GOOS: "linux", Hostname: func() (string, error) { return "n100.local", nil }}
	m.deps.Compose = func(io.Writer, *compose.Outcomes) (composeRunner, error) { return nil, errors.New("no Docker here") }
	return f
}

func writeClonedConfig(t *testing.T, config string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(config, "secrets"), 0o755))
	files := map[string]string{
		"installation.env": "INSTALLATION_NAME=gorgon\nTZ=Europe/London\nRESTIC_REPOSITORY=/backups\n",
		"config.yml":       "config: 0\n",
		"images.yml":       "services:\n  jellyfin:\n    image: j@sha256:x\n",
	}
	for name, text := range files {
		require.NoError(t, os.WriteFile(filepath.Join(config, name), []byte(text), 0o644))
	}
	rules := "creation_rules:\n  - path_regex: (^|/)secrets/[^/]+\\.sops\\.env$\n    age: " + testRecipient + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(config, ".sops.yaml"), []byte(rules), 0o644))
	encrypt := secrets.SopsEncrypter(t.TempDir(), filepath.Join(config, ".sops.yaml"))
	for name, plain := range map[string]string{
		"secrets/apps.sops.env":   "SONARR_API_KEY=s\nRADARR_API_KEY=r\nPROWLARR_API_KEY=p\nJELLYFIN_ADMIN_PASSWORD=j\nDELUGE_WEB_PASSWORD=d\nPORTAINER_ADMIN_PASSWORD=twelve-chars\n",
		"secrets/backup.sops.env": "RESTIC_PASSWORD=restic\n",
		"secrets/vpn.sops.env":    "VPN_SERVICE_PROVIDER=protonvpn\n",
	} {
		path := filepath.Join(config, name)
		sealed, err := encrypt(path, []byte(plain))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, sealed, 0o644))
	}
}

func (f *joinFixture) join(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	root := NewRootCommand(f.deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	code := run(context.Background(), root, append([]string{"setup"}, args...))
	return code, stdout.String(), stderr.String()
}

func (f *joinFixture) repositoryExists() {
	f.repositories.EXPECT().Login(mock.Anything).Return("pablovarela", nil).Once()
	f.repositories.EXPECT().RepositoryExists(mock.Anything, "pablovarela", "media-server-config-gorgon").Return(true, nil).Once()
}

func (f *joinFixture) pastesTheKey() {
	f.prompter.EXPECT().Secret("gorgon's secrets key", mock.Anything, mock.Anything).RunAndReturn(
		func(_, _ string, validate func(string) error) (string, error) {
			if err := validate("not a key"); err == nil {
				return "", errors.New("the prompt accepted something that isn't a key")
			}
			return f.secret, validate(f.secret)
		}).Once()
}

func TestSetupRebuildingStopsBeforeWritingAnything(t *testing.T) {
	tests := map[string]struct {
		given func(t *testing.T, f *joinFixture)
		args  []string
		err   string
	}{
		"no terminal": {given: func(_ *testing.T, f *joinFixture) { f.deps.Terminal = func() bool { return false } },
			err: "mse setup needs a terminal; run it from an interactive shell (over ssh: ssh -t)"},
		"bad name":       {args: []string{"Gorgon"}, err: "the installation name \"Gorgon\" must start with a lowercase letter"},
		"--installation": {args: []string{"gorgon", "--installation", "gorgon"}, err: "mse setup takes the installation's name as its argument; it has no --installation"},
		"another installation here": {given: func(t *testing.T, f *joinFixture) {
			other := filepath.Join(f.deps.Home, ".config", "mse", "medusa")
			require.NoError(t, os.MkdirAll(other, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(other, "installation.env"), []byte("INSTALLATION_NAME=medusa\n"), 0o644))
		}, err: "this machine already runs the installation medusa"},
		"already here": {given: func(t *testing.T, f *joinFixture) { require.NoError(t, os.MkdirAll(f.config, 0o755)) },
			err: "gorgon is already set up on this machine; mse status shows its state"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newJoinFixture(t)
			if tt.given != nil {
				tt.given(t, f)
			}
			args := tt.args
			if args == nil {
				args = []string{"gorgon"}
			}

			code, _, stderr := f.join(t, args...)

			assert.Equal(t, 1, code)
			assert.Contains(t, stderr, strings.ReplaceAll(tt.err, "<config>", f.config))
			assert.NoFileExists(t, f.keys)
		})
	}
}

func TestSetupRebuildingStopsOnAMachineThatIsNotReady(t *testing.T) {
	f := newJoinFixture(t)
	delete(f.answers, "loginctl show-user pablo -p Linger")

	code, stdout, stderr := f.join(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Contains(t, stdout, "1 thing to fix. Run mse check-machine again afterwards.")
	assert.Empty(t, stderr)
	assert.NoDirExists(t, f.config)
}

func TestSetupRebuildingHintsAtTheHomepagePortWhenPortsAreTaken(t *testing.T) {
	f := newJoinFixture(t)
	f.deps.PortFree = func(p machine.Port) bool { return p.String() != "80" }

	_, stdout, _ := f.join(t, "gorgon")

	assert.Contains(t, stdout, "If port 80 is in use by something you keep: a new installation takes another port with mse setup gorgon --homepage-port <port>; an existing one changes its homepage port with mse configure on a machine that has it.\n")
}

func TestSetupRebuildingKeepsNothingWhenTheCloneFails(t *testing.T) {
	f := newJoinFixture(t)
	f.repositoryExists()
	f.answers["git clone --quiet -- "+joinCloneURL+" "+f.config] = process.Result{Exit: 128, Stderr: []byte("fatal: repository not found\n")}
	f.effects["git clone --quiet -- "+joinCloneURL+" "+f.config] = func() { require.NoError(t, os.MkdirAll(f.config, 0o755)) }

	code, _, stderr := f.join(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Equal(t, "mse: git clone failed (exit 128): fatal: repository not found; check gh auth status and that this account can read pablovarela/media-server-config-gorgon\n"+
		"Nothing was kept; run mse setup gorgon again.\n", stderr)
	assert.NoDirExists(t, f.config)
}

func TestSetupRebuildingUndoesWhenTheKeyIsNotGiven(t *testing.T) {
	f := newJoinFixture(t)
	f.repositoryExists()
	f.prompter.EXPECT().Secret(mock.Anything, mock.Anything, mock.Anything).Return("", configure.ErrAborted).Once()

	code, _, stderr := f.join(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "mse: stopped before gorgon joined. Nothing was kept apart from this run's log in "+
		filepath.Join(f.deps.Home, ".local", "state", "mse", "gorgon", "logs")+"; run mse setup gorgon again.")
	assert.NoDirExists(t, f.config)
	assert.NoFileExists(t, f.keys)
}

func TestSetupRebuildingRefusesAConfigForANewerMse(t *testing.T) {
	f := newJoinFixture(t)
	f.deps.Build = version.Build{Version: "v0.18.0"}
	f.repositoryExists()
	f.effects["git clone --quiet -- "+joinCloneURL+" "+f.config] = func() {
		writeClonedConfig(t, f.config)
		require.NoError(t, os.WriteFile(filepath.Join(f.config, "config.yml"), []byte("config: 1\n"), 0o644))
	}

	code, _, stderr := f.join(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "mse update --force")
	assert.NoDirExists(t, f.config)
}

func TestSetupRebuildingTakesThePastedKeyAndRestores(t *testing.T) {
	f := newJoinFixture(t)
	f.repositoryExists()
	f.pastesTheKey()

	code, stdout, stderr := f.join(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Contains(t, stdout, "Cloning github.com/pablovarela/media-server-config-gorgon... done")
	assert.Contains(t, stderr, "no Docker here\ngorgon's config and key are on this machine; its data isn't restored. Finish with:\n"+
		"  mse restore --installation gorgon\n")
	keys, err := os.ReadFile(f.keys)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(keys), "# media server gorgon, added 2026-10-06\n# public key: "+testRecipient+"\n"+f.secret+"\n"))
	assert.FileExists(t, filepath.Join(f.deps.Home, ".local", "state", "mse", "gorgon", ".secrets", "apps.env"))
}

func TestSetupRebuildingUsesAKeyAlreadyOnTheMachine(t *testing.T) {
	f := newJoinFixture(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(f.keys), 0o700))
	original := "# an older key\n" + f.secret + "\n"
	require.NoError(t, os.WriteFile(f.keys, []byte(original), 0o600))
	f.repositoryExists()

	code, _, stderr := f.join(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "no Docker here")
	keys, err := os.ReadFile(f.keys)
	require.NoError(t, err)
	assert.Equal(t, original, string(keys))
}

func TestSetupRebuildingRestoresIntoAFreshDataFolder(t *testing.T) {
	f := newJoinFixture(t)
	f.repositoryExists()
	f.pastesTheKey()
	project := &types.Project{Name: "media-server"}
	composer := newMockComposeRunner(t)
	composer.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(project, nil)
	composer.EXPECT().AnyRunning(mock.Anything, project).Return(false, nil)
	composer.EXPECT().Pull(mock.Anything, project).Return(compose.Pulled{}, errors.New("stop at the pull"))
	f.deps.Compose = func(io.Writer, *compose.Outcomes) (composeRunner, error) { return composer, nil }
	f.answers["restic snapshots --no-lock --host gorgon --json"] = process.Result{
		Stdout: []byte(`[{"time":"2026-10-06T04:30:00Z","tags":["machine:other","machine-name:gorgon-pi"]}]`)}
	restored := false
	f.runner.EXPECT().Run(mock.Anything, mock.MatchedBy(func(c process.Command) bool {
		return c.Name == "restic" && len(c.Args) == 1 && c.Args[0] == "unlock"
	})).Return(0, nil).Maybe()
	f.runner.EXPECT().Run(mock.Anything, mock.MatchedBy(func(c process.Command) bool {
		return c.Name == "restic" && len(c.Args) > 0 && c.Args[0] == "restore"
	})).RunAndReturn(func(context.Context, process.Command) (int, error) {
		restored = true
		return 0, nil
	}).Once()

	f.prompter.EXPECT().Ask("Make this machine the main instead?", false).Return(false, nil).Once()

	code, stdout, stderr := f.join(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.True(t, restored, "the latest backup was restored")
	assert.NotContains(t, stdout, "Kept the app data")
	assert.Contains(t, stderr, "stop at the pull")
	assert.Contains(t, stderr, "Finish with:\n  mse apply --installation gorgon")
}

func TestSetupOverwriteMovesTheAppDataAsideAndRestores(t *testing.T) {
	f := newJoinFixture(t)
	f.repositoryExists()
	f.pastesTheKey()
	data := filepath.Join(f.deps.Home, ".local", "share", "mse", "gorgon")
	require.NoError(t, os.MkdirAll(filepath.Join(data, "volumes", "jellyfin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(data, "volumes", "jellyfin", "library.db"), []byte("old"), 0o644))
	project := &types.Project{Name: "media-server"}
	composer := newMockComposeRunner(t)
	composer.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(project, nil)
	composer.EXPECT().AnyRunning(mock.Anything, project).Return(false, nil)
	composer.EXPECT().Pull(mock.Anything, project).Return(compose.Pulled{}, errors.New("stop at the pull"))
	f.deps.Compose = func(io.Writer, *compose.Outcomes) (composeRunner, error) { return composer, nil }
	f.answers["restic snapshots --no-lock --host gorgon --json"] = process.Result{
		Stdout: []byte(`[{"time":"2026-10-06T04:30:00Z","tags":["machine:other","machine-name:gorgon-pi"]}]`)}
	f.runner.EXPECT().Run(mock.Anything, mock.MatchedBy(func(c process.Command) bool {
		return c.Name == "restic" && len(c.Args) == 1 && c.Args[0] == "unlock"
	})).Return(0, nil).Maybe()
	restored := false
	f.runner.EXPECT().Run(mock.Anything, mock.MatchedBy(func(c process.Command) bool {
		return c.Name == "restic" && len(c.Args) > 0 && c.Args[0] == "restore"
	})).RunAndReturn(func(context.Context, process.Command) (int, error) {
		restored = true
		return 0, nil
	}).Once()
	f.prompter.EXPECT().Ask("Make this machine the main instead?", false).Return(false, nil).Once()

	_, stdout, _ := f.join(t, "gorgon", "--overwrite")

	assert.True(t, restored, "the latest backup was restored over the app data")
	assert.Contains(t, stdout, "previous volumes/ kept in "+data+"/volumes.before-restore-")
}

func TestSetupRebuildingKeepsTheLogOfAConfigItCannotLoad(t *testing.T) {
	f := newJoinFixture(t)
	f.repositoryExists()
	f.effects["git clone --quiet -- "+joinCloneURL+" "+f.config] = func() {
		writeClonedConfig(t, f.config)
		require.NoError(t, os.WriteFile(filepath.Join(f.config, "installation.env"), []byte("INSTALLATION_NAME=medusa\n"), 0o644))
	}
	logs := filepath.Join(f.deps.Home, ".local", "state", "mse", "gorgon", "logs")

	code, _, stderr := f.join(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "Nothing was kept apart from this run's log in "+logs+"; run mse setup gorgon again.")
	assert.NotContains(t, stderr, "update --force")
	assert.FileExists(t, filepath.Join(logs, "mse.log"))
	assert.NoDirExists(t, f.config)
}

func TestSetupRebuildingStopsBeforeThePromptWhenTheConfigHasNoSecrets(t *testing.T) {
	f := newJoinFixture(t)
	f.repositoryExists()
	f.effects["git clone --quiet -- "+joinCloneURL+" "+f.config] = func() {
		writeClonedConfig(t, f.config)
		require.NoError(t, os.RemoveAll(filepath.Join(f.config, "secrets")))
	}

	code, _, stderr := f.join(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "the config has no encrypted secrets to check the key against")
	assert.NoDirExists(t, f.config)
}
