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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/configure"
	"github.com/pablovarela/media-server-engine/internal/process"
	"github.com/pablovarela/media-server-engine/internal/secrets"
)

const (
	configureEnv  = "INSTALLATION_NAME=gorgon\nTZ=Europe/London\nJELLYFIN_ADMIN_USER=pablo\nRESTIC_REPOSITORY=b2:bucket\n"
	testRecipient = "age1zxt57qnfrwmcth7uas4mq995ll9rcjdenmcftge67frhhetakejqflm07u"
	notApplied    = "\nThe new configuration is NOT applied on this machine yet.\nTo apply it now, run:\n\n    mse apply\n\nOtherwise every machine applies it at its next nightly update (05:00).\n"
)

var sealedSecrets = map[string]string{
	"secrets/backup.sops.env":       "RESTIC_PASSWORD=restic-pass\nB2_ACCOUNT_ID=b2-id\nB2_ACCOUNT_KEY=b2-key\n",
	"secrets/vpn.sops.env":          "VPN_SERVICE_PROVIDER=protonvpn\nOPENVPN_USER=vpn-user\nOPENVPN_PASSWORD=old-pass\n",
	"secrets/healthchecks.sops.env": "HEALTHCHECKS_PING_KEY=ping\nHEALTHCHECKS_API_KEY=read\nHEALTHCHECKS_MANAGE_KEY=manage\n",
	"secrets/apps.sops.env":         "SONARR_API_KEY=s\nRADARR_API_KEY=r\nPROWLARR_API_KEY=p\nJELLYFIN_ADMIN_PASSWORD=j\nDELUGE_WEB_PASSWORD=d\nPORTAINER_ADMIN_PASSWORD=pt\n",
}

type configureFixture struct {
	home, config string
	runner       *mockCommandRunner
	prompter     *mockPrompter
	deps         Dependencies
	git          []string
	answers      map[string]process.Result
}

func newConfigureFixture(t *testing.T) *configureFixture {
	t.Helper()
	_, home := xdgHome(t, map[string]string{"gorgon": configureEnv})
	key, err := filepath.Abs("../internal/secrets/testdata/age.key")
	require.NoError(t, err)
	for _, variable := range []string{"SOPS_AGE_KEY", "SOPS_AGE_KEY_CMD", "SOPS_AGE_SSH_PRIVATE_KEY_FILE"} {
		t.Setenv(variable, "")
		require.NoError(t, os.Unsetenv(variable))
	}
	t.Setenv("SOPS_AGE_KEY_FILE", key)
	f := &configureFixture{
		home: home, config: filepath.Join(home, ".config", "mse", "gorgon"),
		runner: newMockCommandRunner(t), prompter: newMockPrompter(t),
		answers: map[string]process.Result{
			"config user.name":                   {Stdout: []byte("Pablo\n")},
			"config user.email":                  {Stdout: []byte("p@example.com\n")},
			"remote get-url origin":              {Stdout: []byte("https://github.com/pablovarela/media-server-config-gorgon.git\n")},
			"rev-parse --abbrev-ref @{upstream}": {Stdout: []byte("origin/main\n")},
			"rev-list --count @{upstream}..HEAD": {Stdout: []byte("0\n")},
			"rev-list --count HEAD..@{upstream}": {Stdout: []byte("0\n")},
			"rev-parse --short HEAD":             {Stdout: []byte("a1b2c3d\n")},
		},
	}
	rules := "creation_rules:\n  - path_regex: (^|/)secrets/[^/]+\\.sops\\.env$\n    age: " + testRecipient + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(f.config, ".sops.yaml"), []byte(rules), 0o644))
	encrypt := secrets.SopsEncrypter(t.TempDir(), filepath.Join(f.config, ".sops.yaml"))
	for file, plain := range sealedSecrets {
		path := filepath.Join(f.config, file)
		encrypted, err := encrypt(path, []byte(plain))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, encrypted, 0o644))
	}
	f.runner.EXPECT().Output(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, c process.Command) (process.Result, error) {
		require.Equal(t, "git", c.Name)
		call := strings.Join(c.Args[2:], " ")
		f.git = append(f.git, call)
		return f.answers[call], nil
	}).Maybe()
	f.deps = Dependencies{
		Environment: func(key string) string { return map[string]string{"USER": "pablo"}[key] },
		Home:        home,
		Decrypt:     secrets.Sops(t.TempDir()),
		Encrypt: func(config string) secrets.Encrypter {
			return secrets.SopsEncrypter(t.TempDir(), filepath.Join(config, ".sops.yaml"))
		},
		RandomKey: func() (string, error) { return "0123456789abcdef0123456789abcdef", nil },
		Terminal:  func() bool { return true },
		Prompter:  func(context.Context) prompter { return f.prompter },
		Run:       func(_, _ io.Writer) commandRunner { return f.runner },
	}
	return f
}

func (f *configureFixture) configure(t *testing.T) (int, string, string) {
	t.Helper()
	root := NewRootCommand(f.deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	code := run(context.Background(), root, []string{"configure"})
	return code, stdout.String(), stderr.String()
}

func (f *configureFixture) chooses(choices ...string) {
	for _, choice := range choices {
		f.prompter.EXPECT().Menu("Configure gorgon", mock.Anything).Return(choice, nil).Once()
	}
}

func (f *configureFixture) answersSection(name string, answer map[string]string) {
	f.prompter.EXPECT().Section(name, mock.Anything, mock.Anything, "").RunAndReturn(
		func(_ string, _ []configure.Field, current map[string]string, _ string) (map[string]string, error) {
			merged := map[string]string{}
			for key, value := range current {
				merged[key] = value
			}
			for key, value := range answer {
				merged[key] = value
			}
			return merged, nil
		}).Once()
}

func (f *configureFixture) confirmsSaving() {
	f.prompter.EXPECT().Confirm("Save, commit and push these?", mock.Anything).Return(true, nil).Once()
}

func (f *configureFixture) committed() []string {
	var commits []string
	for _, call := range f.git {
		if strings.HasPrefix(call, "add ") || strings.HasPrefix(call, "commit ") || call == "push --quiet" {
			commits = append(commits, call)
		}
	}
	return commits
}

func TestConfigureNeedsATerminal(t *testing.T) {
	f := newConfigureFixture(t)
	f.deps.Terminal = func() bool { return false }

	code, stdout, stderr := f.configure(t)

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Equal(t, "mse: mse configure needs a terminal; run it from an interactive shell (over ssh: ssh -t)\n", stderr)
	assert.Empty(t, f.git)
}

func TestConfigureNeedsAGitIdentity(t *testing.T) {
	f := newConfigureFixture(t)
	f.answers["config user.email"] = process.Result{Exit: 1}

	code, _, stderr := f.configure(t)

	assert.Equal(t, 1, code)
	assert.Equal(t, "mse: git has no name or email to commit the config with; set them, then run mse configure again:\n"+
		"  git -C "+f.config+" config user.name \"Your Name\"\n"+
		"  git -C "+f.config+" config user.email you@example.com\n", stderr)
}

func TestConfigureRefusesUncommittedChanges(t *testing.T) {
	f := newConfigureFixture(t)
	f.answers["status --porcelain --untracked-files=no"] = process.Result{Stdout: []byte(" M apps.yml\n")}

	code, _, stderr := f.configure(t)

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "The config has changes that are not committed:\n M apps.yml\n")
	assert.Contains(t, stderr, "Commit and push them, then configure again:")
}

func TestConfigureRefusesADivergedConfig(t *testing.T) {
	f := newConfigureFixture(t)
	f.answers["rev-list --count @{upstream}..HEAD"] = process.Result{Stdout: []byte("1\n")}
	f.answers["rev-list --count HEAD..@{upstream}"] = process.Result{Stdout: []byte("2\n")}

	code, stdout, stderr := f.configure(t)

	assert.Equal(t, 1, code)
	assert.Equal(t, "Updating the config... failed.\n", stdout)
	assert.Contains(t, stderr, "the config has commits its remote doesn't have")
}

func TestConfigureNamesASecretFileItCannotRead(t *testing.T) {
	f := newConfigureFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(f.config, "secrets", "vpn.sops.env"), []byte("not sops\n"), 0o644))

	code, _, stderr := f.configure(t)

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "mse: could not read secrets/vpn.sops.env: ")
}

func TestConfigureWithNothingChanged(t *testing.T) {
	f := newConfigureFixture(t)
	f.chooses("save")

	code, stdout, stderr := f.configure(t)

	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "Updating the config... up to date.\nNothing changed.\n", stdout)
	assert.Empty(t, f.committed())
}

func TestConfigureSavesCommitsAndPushesATimeZone(t *testing.T) {
	f := newConfigureFixture(t)
	f.chooses("General", "save")
	f.answersSection("General", map[string]string{"TZ": "Europe/Madrid"})
	f.confirmsSaving()

	code, stdout, stderr := f.configure(t)

	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "Updating the config... up to date.\n"+
		"Writing installation.env... done.\n"+
		"Committing the config... a1b2c3d \"Configure gorgon: General\".\n"+
		"Pushing to github.com/pablovarela/media-server-config-gorgon... done.\n"+notApplied, stdout)
	assert.Equal(t, []string{
		"add -- installation.env",
		"commit --quiet -m Configure gorgon: General -- installation.env",
		"push --quiet",
	}, f.committed())
	text, err := os.ReadFile(filepath.Join(f.config, "installation.env"))
	require.NoError(t, err)
	assert.Equal(t, strings.Replace(configureEnv, "Europe/London", "Europe/Madrid", 1), string(text))
}

func TestConfigureCommitsOnlyTheChangedSecretFile(t *testing.T) {
	f := newConfigureFixture(t)
	f.chooses("VPN", "save")
	f.answersSection("VPN", map[string]string{"OPENVPN_PASSWORD": "n3w-pass"})
	f.confirmsSaving()

	code, stdout, stderr := f.configure(t)

	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "Updating the config... up to date.\n"+
		"Encrypting secrets/vpn.sops.env... done.\n"+
		"Committing the config... a1b2c3d \"Configure gorgon: VPN\".\n"+
		"Pushing to github.com/pablovarela/media-server-config-gorgon... done.\n"+notApplied, stdout)
	assert.Equal(t, []string{
		"add -- secrets/vpn.sops.env",
		"commit --quiet -m Configure gorgon: VPN -- secrets/vpn.sops.env",
		"push --quiet",
	}, f.committed())
	assert.NotContains(t, stdout+stderr, "n3w-pass")
	decrypted, err := secrets.Sops(t.TempDir())(filepath.Join(f.config, "secrets", "vpn.sops.env"))
	require.NoError(t, err)
	assert.Equal(t, "n3w-pass", secrets.Dotenv(decrypted)["OPENVPN_PASSWORD"])
}

func TestConfigureWithoutARemoteCommitsOnly(t *testing.T) {
	f := newConfigureFixture(t)
	f.answers["remote get-url origin"] = process.Result{Exit: 2}
	f.chooses("General", "save")
	f.answersSection("General", map[string]string{"TZ": "Europe/Madrid"})
	f.confirmsSaving()

	code, stdout, stderr := f.configure(t)

	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "Updating the config... no remote, not pulled.\n"+
		"Writing installation.env... done.\n"+
		"Committing the config... a1b2c3d \"Configure gorgon: General\".\n"+
		"Pushing... no remote, not pushed.\n"+notApplied, stdout)
	assert.NotContains(t, f.committed(), "push --quiet")
}

func TestConfigureKeepsTheCommitWhenThePushFails(t *testing.T) {
	f := newConfigureFixture(t)
	f.answers["push --quiet"] = process.Result{Exit: 1, Stderr: []byte("! [rejected] main -> main (fetch first)\n")}
	f.chooses("General", "save")
	f.answersSection("General", map[string]string{"TZ": "Europe/Madrid"})
	f.confirmsSaving()

	code, stdout, stderr := f.configure(t)

	assert.Equal(t, 1, code)
	assert.Equal(t, "Updating the config... up to date.\n"+
		"Writing installation.env... done.\n"+
		"Committing the config... a1b2c3d \"Configure gorgon: General\".\n"+
		"Pushing to github.com/pablovarela/media-server-config-gorgon... failed.\n"+
		"The change is committed here but not pushed: git push --quiet failed (exit 1): ! [rejected] main -> main (fetch first)\n"+
		"Push it with: git -C "+f.config+" push\n"+notApplied, stdout)
	assert.Equal(t, "mse: the config is committed but not pushed: git push --quiet failed (exit 1): ! [rejected] main -> main (fetch first)\n", stderr)
}

func TestConfigureRestoresTheFilesWhenEncryptingFails(t *testing.T) {
	f := newConfigureFixture(t)
	f.deps.Encrypt = func(string) secrets.Encrypter {
		return func(string, []byte) ([]byte, error) { return nil, errors.New("could not make a data key") }
	}
	f.chooses("General", "VPN", "save")
	f.answersSection("General", map[string]string{"TZ": "Europe/Madrid"})
	f.answersSection("VPN", map[string]string{"OPENVPN_PASSWORD": "n3w-pass"})
	f.confirmsSaving()

	code, stdout, stderr := f.configure(t)

	assert.Equal(t, 1, code)
	assert.Equal(t, "Updating the config... up to date.\nWriting installation.env... done.\nEncrypting secrets/vpn.sops.env... failed.\n", stdout)
	assert.Equal(t, "mse: could not encrypt secrets/vpn.sops.env: could not make a data key; the config is back as it was\n", stderr)
	text, err := os.ReadFile(filepath.Join(f.config, "installation.env"))
	require.NoError(t, err)
	assert.Equal(t, configureEnv, string(text))
	assert.Empty(t, f.committed())
}

func TestConfigureDiscardedWritesNothing(t *testing.T) {
	f := newConfigureFixture(t)
	f.chooses("General", "quit")
	f.answersSection("General", map[string]string{"TZ": "Europe/Madrid"})
	f.prompter.EXPECT().Confirm("Discard 1 changes?", []string(nil)).Return(true, nil).Once()

	code, stdout, stderr := f.configure(t)

	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "Updating the config... up to date.\n", stdout)
	assert.Empty(t, f.committed())
}

func TestConfigurePutsTheConfigBackWhenTheCommitFails(t *testing.T) {
	f := newConfigureFixture(t)
	f.answers["commit --quiet -m Configure gorgon: General -- installation.env"] = process.Result{Exit: 1, Stderr: []byte("error: gpg failed to sign the data\n")}
	f.chooses("General", "save")
	f.answersSection("General", map[string]string{"TZ": "Europe/Madrid"})
	f.confirmsSaving()

	code, stdout, stderr := f.configure(t)

	assert.Equal(t, 1, code)
	assert.Equal(t, "Updating the config... up to date.\nWriting installation.env... done.\nCommitting the config... failed.\n", stdout)
	assert.Equal(t, "mse: git commit --quiet failed (exit 1): error: gpg failed to sign the data; the config is back as it was\n", stderr)
	assert.Contains(t, f.git, "reset --quiet -- installation.env")
	text, err := os.ReadFile(filepath.Join(f.config, "installation.env"))
	require.NoError(t, err)
	assert.Equal(t, configureEnv, string(text))
}
