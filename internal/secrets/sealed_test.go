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
		keys            map[string]bool
		err             string
	}{
		"its key names, sorted":   {sealedFor: testRecipient, rule: testRecipient, keys: map[string]bool{"B2_ACCOUNT_ID": true, "RESTIC_PASSWORD": true}},
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

func TestSealedKeysOfValuesAddedByHand(t *testing.T) {
	tests := map[string]struct {
		added string
		keys  map[string]bool
		err   string
	}{
		"a value added in plain text": {added: "SERVER_COUNTRIES=Netherlands\n", err: "secrets/backup.sops.env: SERVER_COUNTRIES isn't encrypted; set it with mse configure"},
		"an empty value":              {added: "B2_ACCOUNT_KEY=\n", keys: map[string]bool{"B2_ACCOUNT_ID": true, "B2_ACCOUNT_KEY": false, "RESTIC_PASSWORD": true}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			config, sopsConfig := sealedConfig(t, testRecipient)
			path := filepath.Join(config, "secrets", "backup.sops.env")
			sealed, err := SopsEncrypter(t.TempDir(), sopsConfig)(path, []byte("RESTIC_PASSWORD=p\nB2_ACCOUNT_ID=i\n"))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, append([]byte(tt.added), sealed...), 0o644))

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
