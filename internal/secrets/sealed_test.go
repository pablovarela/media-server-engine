package secrets

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const otherRecipient = "age1je2wt6rq7cq9j6xgcjl8ghlvd4599x0anvpddxsgdhfw2s7ah5tsms2ner"

func sealedConfig(t *testing.T, recipient string) (config, sopsConfig string) {
	t.Helper()
	config = t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(config, "secrets"), 0o755))
	sopsConfig = filepath.Join(config, ".sops.yaml")
	require.NoError(t, os.WriteFile(sopsConfig, []byte("creation_rules:\n  - path_regex: (^|/)secrets/[^/]+\\.sops\\.env$\n    age: "+recipient+"\n"), 0o644))
	return config, sopsConfig
}

func TestSealedKeys(t *testing.T) {
	tests := map[string]struct {
		sealedFor, rule string
		plain           bool
		keys            []string
		err             string
	}{
		"its key names, sorted":   {sealedFor: testRecipient, rule: testRecipient, keys: []string{"B2_ACCOUNT_ID", "RESTIC_PASSWORD"}},
		"another recipient":       {sealedFor: otherRecipient, rule: testRecipient, err: "secrets/backup.sops.env is encrypted for " + otherRecipient + ", not for " + testRecipient + " as .sops.yaml says"},
		"committed in plain text": {plain: true, rule: testRecipient, err: "secrets/backup.sops.env isn't encrypted by sops"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			config, sopsConfig := sealedConfig(t, tt.rule)
			path := filepath.Join(config, "secrets", "backup.sops.env")
			plain := []byte("RESTIC_PASSWORD=p\nB2_ACCOUNT_ID=i\n")
			text := plain
			if !tt.plain {
				_, encryptWith := sealedConfig(t, tt.sealedFor)
				var err error
				text, err = SopsEncrypter(t.TempDir(), encryptWith)(path, plain)
				require.NoError(t, err)
			}
			require.NoError(t, os.WriteFile(path, text, 0o644))

			keys, err := SealedKeys(sopsConfig, path)

			if tt.err != "" {
				assert.EqualError(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.keys, keys)
		})
	}
}
