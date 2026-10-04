package secrets

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSops(t *testing.T) {
	key, err := filepath.Abs("testdata/age.key")
	require.NoError(t, err)
	t.Setenv("SOPS_AGE_KEY_FILE", key)

	text, err := Sops("testdata/apps.sops.env")

	require.NoError(t, err)
	assert.Equal(t, map[string]string{"JELLYFIN_ADMIN_PASSWORD": `pa=ss"word`, "SONARR_API_KEY": "sonarr-key"}, Dotenv(text))
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
