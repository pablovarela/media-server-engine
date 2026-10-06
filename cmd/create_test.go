package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/configure"
	"github.com/pablovarela/media-server-engine/internal/github"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/machine"
	"github.com/pablovarela/media-server-engine/internal/process"
	"github.com/pablovarela/media-server-engine/internal/secrets"
)

type createFixture struct {
	*checkMachineFixture
	repositories *mockConfigRepositories
	prompter     *mockPrompter
	config, keys string
	homepagePort string
}

func newCreateFixture(t *testing.T) *createFixture {
	t.Helper()
	m := newCheckMachineFixture(t, nil)
	for _, variable := range []string{"SOPS_AGE_KEY", "SOPS_AGE_KEY_CMD", "SOPS_AGE_KEY_FILE"} {
		t.Setenv(variable, "")
		require.NoError(t, os.Unsetenv(variable))
	}
	f := &createFixture{
		checkMachineFixture: m,
		repositories:        newMockConfigRepositories(t),
		prompter:            newMockPrompter(t),
		config:              filepath.Join(m.deps.Home, ".config", "mse", "gorgon"),
		keys:                filepath.Join(m.deps.Home, ".config", "sops", "age", "keys.txt"),
	}
	m.answers["git -C "+m.deps.Home+" config user.name"] = process.Result{Stdout: []byte("Pablo\n")}
	m.answers["git -C "+m.deps.Home+" config user.email"] = process.Result{Stdout: []byte("p@example.com\n")}
	m.answers["git -C "+f.config+" rev-parse --short HEAD"] = process.Result{Stdout: []byte("a1b2c3d\n")}
	for _, ok := range []string{"init --quiet --initial-branch=main", "add -- .", "commit --quiet -m Create gorgon -- .",
		"remote add origin https://github.com/pablovarela/media-server-config-gorgon.git", "push --quiet --set-upstream origin main"} {
		m.answers["git -C "+f.config+" "+ok] = process.Result{}
	}
	engine := m.deps.Engine.(fstest.MapFS)
	engine["config-template/.gitignore"] = &fstest.MapFile{Data: []byte("secrets/*\n!secrets/*.sops.env\n")}
	engine["config-template/config.yml"] = &fstest.MapFile{Data: []byte("config: 0\n")}
	engine["config-template/images.yml"] = &fstest.MapFile{Data: []byte("services:\n  jellyfin:\n    image: j@sha256:x\n")}
	engine["docker-compose.monitoring.yml"] = &fstest.MapFile{Data: []byte("services: {}\n")}
	engine["grafana/grafana.ini"] = &fstest.MapFile{Data: []byte("\n")}
	engine["prometheus/prometheus.yml"] = &fstest.MapFile{Data: []byte("\n")}
	m.deps.Repositories = f.repositories
	m.deps.Prompter = func(context.Context) prompter { return f.prompter }
	m.deps.Decrypt = secrets.Sops(filepath.Join(m.deps.Home, ".config"))
	m.deps.Encrypt = func(config string) secrets.Encrypter {
		return secrets.SopsEncrypter(t.TempDir(), filepath.Join(config, ".sops.yaml"))
	}
	m.deps.RandomKey = func() (string, error) { return "0123456789abcdef0123456789abcdef", nil }
	m.deps.Terminal = func() bool { return true }
	m.deps.Now = func() time.Time { return time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC) }
	m.deps.Host = installation.Host{GOOS: "linux", Hostname: func() (string, error) { return "pi.local", nil }}
	return f
}

func (f *createFixture) create(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	root := NewRootCommand(f.deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	code := run(context.Background(), root, append([]string{"create"}, args...))
	return code, stdout.String(), stderr.String()
}

func (f *createFixture) nameIsFree() {
	f.repositories.EXPECT().Login(mock.Anything).Return("pablovarela", nil).Once()
	f.repositories.EXPECT().RepositoryExists(mock.Anything, "pablovarela", "media-server-config-gorgon").Return(false, nil).Once()
}

func (f *createFixture) savesTheKey() {
	f.prompter.EXPECT().Acknowledge("Saved gorgon's secrets key?", mock.Anything, "saved").RunAndReturn(func(_, text, _ string) error {
		if strings.Contains(text, "AGE-SECRET-KEY-") {
			return errors.New("the key must not be inside the form")
		}
		return nil
	}).Once()
}

func (f *createFixture) answersEverySection() {
	general := map[string]string{"TZ": "Europe/London", "JELLYFIN_ADMIN_USER": "pablo"}
	if f.homepagePort != "" {
		general["HOMEPAGE_PORT"] = f.homepagePort
	}
	answers := map[string]map[string]string{
		"General":    general,
		"Backups":    {"RESTIC_REPOSITORY": "/backups", "RESTIC_PASSWORD": "restic"},
		"VPN":        {"VPN_SERVICE_PROVIDER": "protonvpn"},
		"App logins": {"JELLYFIN_ADMIN_PASSWORD": "j", "DELUGE_WEB_PASSWORD": "d", "PORTAINER_ADMIN_PASSWORD": "twelve-chars"},
	}
	for _, section := range configure.Sections() {
		answer := answers[section.Name]
		f.prompter.EXPECT().Section(section.Name, mock.Anything, mock.Anything).RunAndReturn(
			func(_ string, fields []configure.Field, form configure.Form) (map[string]string, error) {
				merged := map[string]string{}
				for _, field := range fields {
					merged[field.Key] = form.Values[field.Key]
				}
				for k, v := range answer {
					merged[k] = v
				}
				return merged, nil
			}).Once()
	}
}

func (f *createFixture) nothingKept(t *testing.T) {
	t.Helper()
	assert.NoDirExists(t, f.config)
	assert.NoFileExists(t, f.keys)
}

func TestCreateNeedsATerminal(t *testing.T) {
	f := newCreateFixture(t)
	f.deps.Terminal = func() bool { return false }

	code, _, stderr := f.create(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Equal(t, "mse: mse create needs a terminal; run it from an interactive shell (over ssh: ssh -t)\n", stderr)
	f.nothingKept(t)
}

func TestCreateRefusesAnInvalidName(t *testing.T) {
	f := newCreateFixture(t)

	code, stdout, stderr := f.create(t, "Gorgon")

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "the installation name \"Gorgon\" must start with a lowercase letter")
}

func TestCreateStopsOnAMachineThatIsNotReady(t *testing.T) {
	f := newCreateFixture(t)
	delete(f.answers, "restic version")

	code, stdout, stderr := f.create(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Contains(t, stdout, "1 thing to fix. Run mse check-machine again afterwards.")
	assert.Empty(t, stderr)
	f.nothingKept(t)
}

func TestCreateRefusesATakenName(t *testing.T) {
	tests := map[string]struct {
		given func(t *testing.T, f *createFixture)
		err   string
	}{
		"config folder": {
			given: func(t *testing.T, f *createFixture) { require.NoError(t, os.MkdirAll(f.config, 0o755)) },
			err:   "already exists",
		},
		"data folder": {
			given: func(t *testing.T, f *createFixture) {
				require.NoError(t, os.MkdirAll(filepath.Join(f.deps.Home, ".local", "share", "mse", "gorgon"), 0o755))
			},
			err: "already exists",
		},
		"repository": {
			given: func(_ *testing.T, f *createFixture) {
				f.repositories.EXPECT().Login(mock.Anything).Return("pablovarela", nil).Once()
				f.repositories.EXPECT().RepositoryExists(mock.Anything, "pablovarela", "media-server-config-gorgon").Return(true, nil).Once()
			},
			err: "pablovarela/media-server-config-gorgon already exists on GitHub; to add this machine to it, run make join-installation NAME=gorgon from a clone of the engine",
		},
		"git identity": {
			given: func(_ *testing.T, f *createFixture) { delete(f.answers, "git -C "+f.deps.Home+" config user.email") },
			err:   "git config --global user.email you@example.com",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newCreateFixture(t)
			tt.given(t, f)

			code, _, stderr := f.create(t, "gorgon")

			assert.Equal(t, 1, code)
			assert.Contains(t, stderr, tt.err)
			assert.NoFileExists(t, f.keys)
		})
	}
}

func TestCreateUndoesWhenTheKeyIsNotSaved(t *testing.T) {
	f := newCreateFixture(t)
	f.nameIsFree()
	f.prompter.EXPECT().Acknowledge(mock.Anything, mock.Anything, "saved").Return(configure.ErrAborted).Once()

	code, _, stderr := f.create(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Equal(t, "mse: stopped before gorgon was created. Nothing was kept; run mse create gorgon again.\n"+
		"The secrets key shown for gorgon was removed; if you saved it, delete it from your password manager.\n", stderr)
	f.nothingKept(t)
}

func TestCreateUndoesWhenSettingsAreQuit(t *testing.T) {
	f := newCreateFixture(t)
	f.nameIsFree()
	f.savesTheKey()
	f.prompter.EXPECT().Section("General", mock.Anything, mock.Anything).Return(nil, configure.ErrAborted).Once()

	code, _, stderr := f.create(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "Nothing was kept apart from this run's log in "+filepath.Join(f.deps.Home, ".local", "state", "mse", "gorgon", "logs")+"; run mse create gorgon again.")
	f.nothingKept(t)
}

func TestCreateWritesCommitsAndPublishes(t *testing.T) {
	f := newCreateFixture(t)
	f.nameIsFree()
	f.savesTheKey()
	f.answersEverySection()
	f.prompter.EXPECT().Confirm("Save these, commit and push them to a new repository?", mock.Anything).Return(true, nil).Once()
	f.repositories.EXPECT().CreatePrivateRepository(mock.Anything, "", "media-server-config-gorgon").
		Return("https://github.com/pablovarela/media-server-config-gorgon.git", nil).Once()
	f.deps.Compose = func(io.Writer, *compose.Outcomes) (composeRunner, error) { return nil, errors.New("no Docker here") }

	code, stdout, stderr := f.create(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Contains(t, stdout, "Creating github.com/pablovarela/media-server-config-gorgon (private)... done")
	assert.Contains(t, stderr, "no Docker here\ngorgon is created and its config pushed. Finish with:\n  mse claim-backup-main --installation gorgon\n  mse apply --installation gorgon")
	assert.FileExists(t, filepath.Join(f.deps.Home, ".local", "state", "mse", "gorgon", ".secrets", "apps.env"))
	env, err := os.ReadFile(filepath.Join(f.config, "installation.env"))
	require.NoError(t, err)
	assert.Contains(t, string(env), "INSTALLATION_NAME=gorgon\n")
	assert.Contains(t, string(env), "TZ=Europe/London")
	apps, err := f.deps.Decrypt(filepath.Join(f.config, "secrets", "apps.sops.env"))
	require.NoError(t, err)
	assert.Contains(t, string(apps), "SONARR_API_KEY=0123456789abcdef0123456789abcdef")
	keys, err := os.ReadFile(f.keys)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(keys), "# media server gorgon, created 2026-10-06\n"))
	secret := strings.Split(string(keys), "\n")[2]
	assert.Contains(t, stdout, "\n    "+secret+"\n")
	log, err := os.ReadFile(filepath.Join(f.deps.Home, ".local", "state", "mse", "gorgon", "logs", "mse.log"))
	require.NoError(t, err)
	assert.NotContains(t, string(log), "AGE-SECRET-KEY-")
}

func TestCreateRefusesTheInstallationFlag(t *testing.T) {
	f := newCreateFixture(t)

	code, stdout, stderr := f.create(t, "gorgon", "--installation", "gorgon")

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Equal(t, "mse: mse create takes the installation's name as its argument; it has no --installation\n", stderr)
}

func TestCreateRefusesASecondInstallationOnThisMachine(t *testing.T) {
	f := newCreateFixture(t)
	other := filepath.Join(f.deps.Home, ".config", "mse", "gorgon")
	require.NoError(t, os.MkdirAll(other, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(other, "installation.env"), []byte("INSTALLATION_NAME=gorgon\n"), 0o644))

	code, stdout, stderr := f.create(t, "medusa")

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Equal(t, "mse: this machine already runs the installation gorgon, and a machine runs one installation's stack\n", stderr)
	assert.NoFileExists(t, f.keys)
}

func TestCreateIgnoresTheDefaultInstallation(t *testing.T) {
	f := newCreateFixture(t)
	f.deps.Environment = func(key string) string { return map[string]string{"MSE_INSTALLATION": "gorgon"}[key] }
	f.repositories.EXPECT().Login(mock.Anything).Return("", errors.New("stop here")).Once()

	_, stdout, stderr := f.create(t, "gorgon")

	assert.Contains(t, stdout, "This machine is ready.")
	assert.Equal(t, "mse: stop here\n", stderr)
}

func TestCreateChecksAndSetsTheHomepagePortItIsGiven(t *testing.T) {
	f := newCreateFixture(t)
	f.nameIsFree()
	f.savesTheKey()
	f.prompter.EXPECT().Section("General", mock.Anything, mock.Anything).RunAndReturn(
		func(_ string, _ []configure.Field, form configure.Form) (map[string]string, error) {
			assert.Equal(t, "8080", form.Values["HOMEPAGE_PORT"])
			return nil, configure.ErrAborted
		}).Once()

	code, _, _ := f.create(t, "gorgon", "--homepage-port", "8080")

	assert.Equal(t, 1, code)
	var checked []string
	for _, p := range f.checked {
		checked = append(checked, p.String())
	}
	assert.Contains(t, checked, "8080")
	assert.NotContains(t, checked, "80")
}

func TestCreateRefusesABadHomepagePort(t *testing.T) {
	f := newCreateFixture(t)

	code, stdout, stderr := f.create(t, "gorgon", "--homepage-port", "eighty")

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "mse: --homepage-port: ")
}

func TestCreateSuggestsAnotherHomepagePortWhenThePortsAreTaken(t *testing.T) {
	f := newCreateFixture(t)
	f.deps.PortFree = func(p machine.Port) bool { return p.String() != "80" }

	code, stdout, _ := f.create(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Contains(t, stdout, "If port 80 is in use by something you keep, give the landing page another port: mse create gorgon --homepage-port <port>\n")
}

func (f *createFixture) reachesPublishing() {
	f.nameIsFree()
	f.savesTheKey()
	f.answersEverySection()
	f.prompter.EXPECT().Confirm(mock.Anything, mock.Anything).Return(true, nil).Once()
}

func TestCreateFindsTheRepositoryAfterALostAnswer(t *testing.T) {
	f := newCreateFixture(t)
	f.reachesPublishing()
	f.repositories.EXPECT().CreatePrivateRepository(mock.Anything, "", "media-server-config-gorgon").Return("", errors.New("timeout")).Once()
	f.repositories.EXPECT().RepositoryExists(mock.Anything, "pablovarela", "media-server-config-gorgon").Return(true, nil).Once()

	code, _, stderr := f.create(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "mse: timeout\nAdd its remote: git -C "+f.config+" remote add origin https://github.com/pablovarela/media-server-config-gorgon.git\n"+
		"gorgon's repository exists and its config is committed in "+f.config+", but not pushed.")
	assert.DirExists(t, f.config)
	assert.FileExists(t, f.keys)
}

func TestCreateDiscardsWhenTheRepositoryWasNotCreated(t *testing.T) {
	f := newCreateFixture(t)
	f.reachesPublishing()
	f.repositories.EXPECT().CreatePrivateRepository(mock.Anything, "", "media-server-config-gorgon").Return("", errors.New("timeout")).Once()
	f.repositories.EXPECT().RepositoryExists(mock.Anything, "pablovarela", "media-server-config-gorgon").Return(false, nil).Once()

	code, _, stderr := f.create(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "; run mse create gorgon again.")
	f.nothingKept(t)
}

func TestCreatePointsATakenRepositoryAtJoin(t *testing.T) {
	f := newCreateFixture(t)
	f.reachesPublishing()
	f.repositories.EXPECT().CreatePrivateRepository(mock.Anything, "", "media-server-config-gorgon").
		Return("", fmt.Errorf("create media-server-config-gorgon: %w", github.ErrRepositoryTaken)).Once()

	code, _, stderr := f.create(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "To add this machine to that installation, run make join-installation NAME=gorgon from a clone of the engine; otherwise choose another name.")
	f.nothingKept(t)
}

func TestCreateTakesTheOwnerInAnyCase(t *testing.T) {
	f := newCreateFixture(t)
	f.nameIsFree()
	f.prompter.EXPECT().Acknowledge(mock.Anything, mock.Anything, "saved").Return(configure.ErrAborted).Once()

	code, _, _ := f.create(t, "gorgon", "--owner", "PabloVarela")

	assert.Equal(t, 1, code)
}

func TestCreateSummary(t *testing.T) {
	f := newCreateFixture(t)
	_, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\nHOMEPAGE_PORT=8080\n"})
	f.deps.Home = home
	root := NewRootCommand(f.deps)
	cmd, _, err := root.Find([]string{"create"})
	require.NoError(t, err)
	cmd.SetContext(context.Background())
	require.NoError(t, cmd.ParseFlags([]string{"--installation", "gorgon"}))
	c := f.deps.creation(cmd, "gorgon", "")
	c.remote = "github.com/pablovarela/media-server-config-gorgon"

	summary := c.Summary(context.Background())

	assert.Equal(t, "gorgon is ready: its config is in "+filepath.Join(home, ".config", "mse", "gorgon")+" (github.com/pablovarela/media-server-config-gorgon), "+
		"its data in "+filepath.Join(home, ".local", "share", "mse", "gorgon")+" and its landing page at http://pi.local:8080. mse urls lists every app.", summary)
}

func TestCreateKeepsEverythingWhenGitHubCannotSay(t *testing.T) {
	f := newCreateFixture(t)
	f.reachesPublishing()
	f.repositories.EXPECT().CreatePrivateRepository(mock.Anything, "", "media-server-config-gorgon").Return("", errors.New("timeout")).Once()
	f.repositories.EXPECT().RepositoryExists(mock.Anything, "pablovarela", "media-server-config-gorgon").Return(false, errors.New("no network")).Once()

	code, _, stderr := f.create(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "mse: timeout\nGitHub didn't say whether github.com/pablovarela/media-server-config-gorgon was created (no network). "+
		"If it wasn't, create it first: gh repo create pablovarela/media-server-config-gorgon --private\n"+
		"Add its remote: git -C "+f.config+" remote add origin https://github.com/pablovarela/media-server-config-gorgon.git\n")
	assert.DirExists(t, f.config)
	assert.FileExists(t, f.keys)
}

func TestCreateChecksAHomepagePortChangedInTheSettings(t *testing.T) {
	f := newCreateFixture(t)
	f.homepagePort = "8443"
	f.nameIsFree()
	f.savesTheKey()
	f.answersEverySection()
	f.prompter.EXPECT().Confirm(mock.Anything, mock.Anything).Return(true, nil).Once()
	f.deps.PortFree = func(p machine.Port) bool { return p.String() != "8443" }
	f.deps.Published = func(context.Context) ([]machine.Published, error) { return nil, nil }

	code, stdout, stderr := f.create(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Contains(t, stdout, "Checking the landing page's port 8443... failed.")
	assert.Contains(t, stderr, "8443 is in use")
	f.nothingKept(t)
}
