package secrets

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSops(t *testing.T) {
	testKey, err := os.ReadFile("testdata/age.key")
	require.NoError(t, err)
	type Given struct {
		keyFile   bool
		xdgKey    bool
		keyInline bool
	}
	type Then struct {
		err string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"SOPS_AGE_KEY_FILE": {Given: Given{keyFile: true}},
		"SOPS_AGE_KEY":      {Given: Given{keyInline: true}},
		"the key in ~/.config with XDG_CONFIG_HOME unset": {Given: Given{xdgKey: true}},
		"no key anywhere says what sops tried":            {Then: Then{err: "Failed to get the data key required to decrypt the SOPS file"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			config := filepath.Join(home, ".config")
			t.Setenv("HOME", home)
			unset(t, "XDG_CONFIG_HOME")
			for _, variable := range []string{"SOPS_AGE_KEY_FILE", "SOPS_AGE_KEY", "SOPS_AGE_KEY_CMD", "SOPS_AGE_SSH_PRIVATE_KEY_FILE"} {
				unset(t, variable)
			}
			if tt.Given.keyFile {
				path := filepath.Join(home, "elsewhere.key")
				require.NoError(t, os.WriteFile(path, testKey, 0o600))
				t.Setenv("SOPS_AGE_KEY_FILE", path)
			}
			if tt.Given.keyInline {
				t.Setenv("SOPS_AGE_KEY", string(testKey))
			}
			if tt.Given.xdgKey {
				path := filepath.Join(config, "sops", "age", "keys.txt")
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
				require.NoError(t, os.WriteFile(path, testKey, 0o600))
			}

			text, err := Sops(config)("testdata/apps.sops.env")

			if tt.Then.err != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.Then.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, map[string]string{"JELLYFIN_ADMIN_PASSWORD": `pa=ss"word`, "SONARR_API_KEY": "sonarr-key"}, Dotenv(text))
		})
	}
}

func TestDotenv(t *testing.T) {
	tests := map[string]struct {
		Given struct{ text string }
		Then  struct{ values map[string]string }
	}{
		"values keep = and quotes": {
			Given: struct{ text string }{"A=x=y\nB=\"quoted\"\nC='q'\n"},
			Then:  struct{ values map[string]string }{map[string]string{"A": "x=y", "B": `"quoted"`, "C": "'q'"}},
		},
		"blank lines skipped": {
			Given: struct{ text string }{"\nA=1\n\n"},
			Then:  struct{ values map[string]string }{map[string]string{"A": "1"}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.Then.values, Dotenv([]byte(tt.Given.text)))
		})
	}
}

func unset(t *testing.T, variable string) {
	t.Helper()
	t.Setenv(variable, "")
	require.NoError(t, os.Unsetenv(variable))
}
