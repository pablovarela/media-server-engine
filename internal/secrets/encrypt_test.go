package secrets

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testRecipient = "age1zxt57qnfrwmcth7uas4mq995ll9rcjdenmcftge67frhhetakejqflm07u"

func sopsConfigFor(t *testing.T, recipient string) string {
	t.Helper()
	config := t.TempDir()
	rules := "creation_rules:\n  - path_regex: (^|/)secrets/[^/]+\\.sops\\.env$\n    age: " + recipient + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(config, ".sops.yaml"), []byte(rules), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(config, "secrets"), 0o755))
	return config
}

func useTestKey(t *testing.T) {
	t.Helper()
	key, err := filepath.Abs("testdata/age.key")
	require.NoError(t, err)
	for _, variable := range []string{"SOPS_AGE_KEY", "SOPS_AGE_KEY_CMD", "SOPS_AGE_SSH_PRIVATE_KEY_FILE"} {
		unset(t, variable)
	}
	t.Setenv("SOPS_AGE_KEY_FILE", key)
}

func TestSopsEncrypterWritesWhatSopsDecrypts(t *testing.T) {
	useTestKey(t)
	config := sopsConfigFor(t, testRecipient)
	path := filepath.Join(config, "secrets", "vpn.sops.env")
	plain := []byte("VPN_SERVICE_PROVIDER=protonvpn\nOPENVPN_PASSWORD=s3cret\n")

	encrypted, err := SopsEncrypter(t.TempDir(), filepath.Join(config, ".sops.yaml"))(path, plain)

	require.NoError(t, err)
	assert.NotContains(t, string(encrypted), "s3cret")
	require.NoError(t, os.WriteFile(path, encrypted, 0o644))
	decrypted, err := Sops(t.TempDir())(path)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"VPN_SERVICE_PROVIDER": "protonvpn", "OPENVPN_PASSWORD": "s3cret"}, Dotenv(decrypted))
}

func TestSopsEncrypterNeedsACreationRule(t *testing.T) {
	useTestKey(t)
	config := sopsConfigFor(t, testRecipient)

	_, err := SopsEncrypter(t.TempDir(), filepath.Join(config, ".sops.yaml"))(filepath.Join(config, "other.env"), []byte("A=s3cret\n"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no creation rule for")
	assert.NotContains(t, err.Error(), "s3cret")
}

func TestSopsEncrypterKeepsTheRulesCommentSettings(t *testing.T) {
	useTestKey(t)
	config := t.TempDir()
	rules := "creation_rules:\n  - path_regex: (^|/)secrets/[^/]+\\.sops\\.env$\n    age: " + testRecipient + "\n    unencrypted_comment_regex: keep-plain\n"
	require.NoError(t, os.WriteFile(filepath.Join(config, ".sops.yaml"), []byte(rules), 0o644))

	encrypted, err := SopsEncrypter(t.TempDir(), filepath.Join(config, ".sops.yaml"))(filepath.Join(config, "secrets", "vpn.sops.env"), []byte("A=1\n"))

	require.NoError(t, err)
	assert.Contains(t, string(encrypted), "sops_unencrypted_comment_regex=keep-plain")
}
