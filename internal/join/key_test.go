package join

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/create"
	"github.com/pablovarela/media-server-engine/internal/secrets"
)

const testRecipient = "age1zxt57qnfrwmcth7uas4mq995ll9rcjdenmcftge67frhhetakejqflm07u"

func testSecret(t *testing.T) string {
	t.Helper()
	text, err := os.ReadFile("../secrets/testdata/age.key")
	require.NoError(t, err)
	for _, line := range strings.Split(string(text), "\n") {
		if strings.HasPrefix(line, "AGE-SECRET-KEY-") {
			return line
		}
	}
	t.Fatal("no secret in the test key")
	return ""
}

func configWithSecrets(t *testing.T) string {
	t.Helper()
	config := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(config, "secrets"), 0o755))
	sealed, err := os.ReadFile("../secrets/testdata/apps.sops.env")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(config, "secrets", "apps.sops.env"), sealed, 0o644))
	return config
}

func TestRecipients(t *testing.T) {
	recipients, err := Recipients(configWithSecrets(t))

	require.NoError(t, err)
	assert.Equal(t, []string{testRecipient}, recipients)
}

func TestRecipientsOfAConfigWithoutSecrets(t *testing.T) {
	_, err := Recipients(t.TempDir())

	assert.EqualError(t, err, "the config has no encrypted secrets to check the key against")
}

func TestMatchKey(t *testing.T) {
	config := configWithSecrets(t)
	other, err := create.NewKey()
	require.NoError(t, err)
	tests := map[string]struct {
		pasted string
		err    string
	}{
		"the installation's key": {pasted: testSecret(t)},
		"with spaces":            {pasted: "  " + testSecret(t) + " \n"},
		"another key":            {pasted: other.Secret, err: "that key isn't one of this installation's keys"},
		"not a key":              {pasted: "hunter2", err: "that isn't an age secret key; it starts with AGE-SECRET-KEY-1"},
		"empty":                  {pasted: "", err: "that isn't an age secret key; it starts with AGE-SECRET-KEY-1"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			key, err := MatchKey(tt.pasted, config)

			if tt.err != "" {
				assert.EqualError(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testRecipient, key.Public)
			assert.Equal(t, testSecret(t), key.Secret)
		})
	}
}

func TestMatchKeyFindsTheSecretLineInAPastedBlock(t *testing.T) {
	pasted := "# media server gorgon, created 2026-10-01\n# public key: " + testRecipient + "\n" + testSecret(t) + "\n"

	key, err := MatchKey(pasted, configWithSecrets(t))

	require.NoError(t, err)
	assert.Equal(t, testSecret(t), key.Secret)
}

func TestMatchKeyFindsTheSecretInABlockPastedAsOneLine(t *testing.T) {
	pasted := "# media server gorgon, created 2026-10-01 # public key: " + testRecipient + " " + testSecret(t)

	key, err := MatchKey(pasted, configWithSecrets(t))

	require.NoError(t, err)
	assert.Equal(t, testSecret(t), key.Secret)
}

func TestMatchKeyNeedsAKeyThatOpensEverySecretFile(t *testing.T) {
	config := configWithSecrets(t)
	other, err := create.NewKey()
	require.NoError(t, err)
	rules := filepath.Join(t.TempDir(), ".sops.yaml")
	require.NoError(t, os.WriteFile(rules, []byte("creation_rules:\n  - path_regex: \\.sops\\.env$\n    age: "+other.Public+"\n"), 0o644))
	path := filepath.Join(config, "secrets", "vpn.sops.env")
	sealed, err := secrets.SopsEncrypter(t.TempDir(), rules)(path, []byte("VPN_SERVICE_PROVIDER=x\n"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, sealed, 0o644))

	_, err = MatchKey(testSecret(t), config)

	assert.EqualError(t, err, "that key opens only some of this installation's secret files (not secrets/vpn.sops.env)")
}
