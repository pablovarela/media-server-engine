package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/configure"
	"github.com/pablovarela/media-server-engine/internal/process"
	"github.com/pablovarela/media-server-engine/internal/secrets"
)

type createFixture struct {
	*checkMachineFixture
	repositories *mockConfigRepositories
	prompter     *mockPrompter
	config, keys string
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
	m.deps.Repositories = f.repositories
	m.deps.Prompter = func(context.Context) prompter { return f.prompter }
	m.deps.Decrypt = secrets.Sops(filepath.Join(m.deps.Home, ".config"))
	m.deps.Encrypt = func(config string) secrets.Encrypter {
		return secrets.SopsEncrypter(t.TempDir(), filepath.Join(config, ".sops.yaml"))
	}
	m.deps.RandomKey = func() (string, error) { return "0123456789abcdef0123456789abcdef", nil }
	m.deps.Terminal = func() bool { return true }
	m.deps.Now = func() time.Time { return time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC) }
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
	f.prompter.EXPECT().Acknowledge("Secrets key for gorgon", mock.Anything, "saved").Return(nil).Once()
}

func (f *createFixture) answersEverySection() {
	answers := map[string]map[string]string{
		"General":    {"TZ": "Europe/London", "JELLYFIN_ADMIN_USER": "pablo"},
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
			err: "pablovarela/media-server-config-gorgon already exists on GitHub; to add this machine to it, run mse join gorgon",
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
	assert.Equal(t, "mse: stopped before gorgon was created. Nothing was kept; run mse create gorgon again.\n", stderr)
	f.nothingKept(t)
}

func TestCreateUndoesWhenSettingsAreQuit(t *testing.T) {
	f := newCreateFixture(t)
	f.nameIsFree()
	f.savesTheKey()
	f.prompter.EXPECT().Section("General", mock.Anything, mock.Anything).Return(nil, configure.ErrAborted).Once()

	code, _, stderr := f.create(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "Nothing was kept; run mse create gorgon again.")
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
	f.deps.ClaimMain = func(*cobra.Command) error { return errors.New("restic says no") }

	code, stdout, stderr := f.create(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Contains(t, stdout, "Creating github.com/pablovarela/media-server-config-gorgon (private)... done")
	assert.Contains(t, stderr, "restic says no\ngorgon is created and its config pushed. Finish with:\n  mse claim-backup-main\n  mse apply")
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
}

func TestCreateChecksPortsBeforeTheInstallationExists(t *testing.T) {
	f := newCreateFixture(t)
	f.repositories.EXPECT().Login(mock.Anything).Return("", errors.New("stop here")).Once()

	_, stdout, stderr := f.create(t, "gorgon", "--installation", "gorgon")

	assert.Contains(t, stdout, "This machine is ready.")
	assert.Contains(t, stderr, "stop here")
	assert.NotContains(t, stderr, "no installation gorgon")
}
