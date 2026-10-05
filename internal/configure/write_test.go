package configure

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/secrets"
)

const testRecipient = "age1zxt57qnfrwmcth7uas4mq995ll9rcjdenmcftge67frhhetakejqflm07u"

type writeFixture struct {
	config  string
	encrypt secrets.Encrypter
}

func newWriteFixture(t *testing.T) *writeFixture {
	t.Helper()
	key, err := filepath.Abs("../secrets/testdata/age.key")
	require.NoError(t, err)
	for _, variable := range []string{"SOPS_AGE_KEY", "SOPS_AGE_KEY_CMD", "SOPS_AGE_SSH_PRIVATE_KEY_FILE"} {
		t.Setenv(variable, "")
		require.NoError(t, os.Unsetenv(variable))
	}
	t.Setenv("SOPS_AGE_KEY_FILE", key)
	config := t.TempDir()
	rules := "creation_rules:\n  - path_regex: (^|/)secrets/[^/]+\\.sops\\.env$\n    age: " + testRecipient + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(config, ".sops.yaml"), []byte(rules), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(config, PlainFile), []byte(gorgonEnv), 0o644))
	f := &writeFixture{config: config, encrypt: secrets.SopsEncrypter(t.TempDir(), filepath.Join(config, ".sops.yaml"))}
	f.seal(t, vpnFile, "VPN_SERVICE_PROVIDER=protonvpn\nOPENVPN_USER=me\nOPENVPN_PASSWORD=old-pass\n")
	return f
}

func (f *writeFixture) seal(t *testing.T, file, plain string) {
	t.Helper()
	path := filepath.Join(f.config, file)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	encrypted, err := f.encrypt(path, []byte(plain))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, encrypted, 0o644))
}

func (f *writeFixture) load(t *testing.T) (Values, Texts) {
	t.Helper()
	values, texts, err := Load(f.config, secrets.Sops(t.TempDir()))
	require.NoError(t, err)
	return values, texts
}

func (f *writeFixture) writer() Writer {
	return Writer{Config: f.config, Encrypt: f.encrypt}
}

func TestFilesNamesWhatTheChangesTouch(t *testing.T) {
	plain, secretFiles := Files([]Change{
		{Section: "VPN", File: vpnFile, Key: "OPENVPN_PASSWORD"},
		{Section: "Rotate keys", File: AppsFile, Key: "SONARR_API_KEY"},
		{Section: "App logins", File: AppsFile, Key: "DELUGE_WEB_PASSWORD"},
	})

	assert.False(t, plain)
	assert.Equal(t, []string{vpnFile, AppsFile}, secretFiles)
}

func TestOnlyASecretChangeRewritesOnlyItsFile(t *testing.T) {
	f := newWriteFixture(t)
	before, texts := f.load(t)
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(f.config, PlainFile), old, old))
	changes := Diff(before, before.With(vpnFile, "OPENVPN_PASSWORD", "n3w-pass"))

	written, err := f.writer().WriteSecrets(texts, changes)

	require.NoError(t, err)
	assert.Equal(t, []string{vpnFile}, written)
	info, err := os.Stat(filepath.Join(f.config, PlainFile))
	require.NoError(t, err)
	assert.True(t, info.ModTime().Equal(old))
	after, _ := f.load(t)
	assert.Equal(t, map[string]string{"VPN_SERVICE_PROVIDER": "protonvpn", "OPENVPN_USER": "me", "OPENVPN_PASSWORD": "n3w-pass"}, after[vpnFile])
}

func TestAPlainChangeRewritesInstallationEnv(t *testing.T) {
	f := newWriteFixture(t)
	before, texts := f.load(t)

	require.NoError(t, f.writer().WritePlain(texts, Diff(before, before.With(PlainFile, "TZ", "Europe/Madrid"))))

	text, err := os.ReadFile(filepath.Join(f.config, PlainFile))
	require.NoError(t, err)
	assert.Contains(t, string(text), "TZ=Europe/Madrid\n")
	assert.Contains(t, string(text), "RESTIC_REPOSITORY=~/backups  # local for now\n")
}

func TestASecretFileThatDidNotExistIsCreated(t *testing.T) {
	f := newWriteFixture(t)
	before, texts := f.load(t)

	written, err := f.writer().WriteSecrets(texts, Diff(before, before.With(healthchecksFile, "HEALTHCHECKS_PING_KEY", "ping")))

	require.NoError(t, err)
	assert.Equal(t, []string{healthchecksFile}, written)
	after, _ := f.load(t)
	assert.Equal(t, "ping", after.Get(healthchecksFile, "HEALTHCHECKS_PING_KEY"))
}

func TestUndoPutsBackTheBytesItReadAndRemovesCreatedFiles(t *testing.T) {
	f := newWriteFixture(t)
	before, texts := f.load(t)
	plainBefore, err := os.ReadFile(filepath.Join(f.config, PlainFile))
	require.NoError(t, err)
	vpnBefore, err := os.ReadFile(filepath.Join(f.config, vpnFile))
	require.NoError(t, err)
	changes := Diff(before, before.With(PlainFile, "TZ", "Europe/Madrid").
		With(vpnFile, "OPENVPN_PASSWORD", "n3w-pass").
		With(healthchecksFile, "HEALTHCHECKS_PING_KEY", "ping"))
	require.NoError(t, f.writer().WritePlain(texts, changes))
	_, err = f.writer().WriteSecrets(texts, changes)
	require.NoError(t, err)

	require.NoError(t, f.writer().Undo(texts, changes))

	plainAfter, err := os.ReadFile(filepath.Join(f.config, PlainFile))
	require.NoError(t, err)
	assert.Equal(t, string(plainBefore), string(plainAfter))
	vpnAfter, err := os.ReadFile(filepath.Join(f.config, vpnFile))
	require.NoError(t, err)
	assert.Equal(t, string(vpnBefore), string(vpnAfter))
	assert.NoFileExists(t, filepath.Join(f.config, healthchecksFile))
}

func TestUndoAlsoRestoresAFileWhoseWriteFailedHalfway(t *testing.T) {
	f := newWriteFixture(t)
	before, texts := f.load(t)
	plainBefore, err := os.ReadFile(filepath.Join(f.config, PlainFile))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(f.config, PlainFile), []byte("INSTALLATION_NA"), 0o644))

	require.NoError(t, f.writer().Undo(texts, Diff(before, before.With(PlainFile, "TZ", "Europe/Madrid"))))

	plainAfter, err := os.ReadFile(filepath.Join(f.config, PlainFile))
	require.NoError(t, err)
	assert.Equal(t, string(plainBefore), string(plainAfter))
}

func TestAFailingEncryptionStopsAtThatFile(t *testing.T) {
	f := newWriteFixture(t)
	before, texts := f.load(t)
	changes := Diff(before, before.With(healthchecksFile, "HEALTHCHECKS_PING_KEY", "ping").With(AppsFile, "DELUGE_WEB_PASSWORD", "deluge"))
	failing := f.writer()
	encrypted := 0
	failing.Encrypt = func(path string, plain []byte) ([]byte, error) {
		if encrypted++; encrypted == 2 {
			return nil, errors.New("could not make a data key")
		}
		return f.encrypt(path, plain)
	}

	written, err := failing.WriteSecrets(texts, changes)

	require.EqualError(t, err, "could not encrypt secrets/apps.sops.env: could not make a data key")
	assert.Equal(t, []string{healthchecksFile}, written)
}
