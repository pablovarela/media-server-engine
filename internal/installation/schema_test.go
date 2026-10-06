package installation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckSchema(t *testing.T) {
	type Given struct {
		configYML *string
	}
	type When struct {
		engineMajor int
	}
	type Then struct {
		err string
	}
	text := func(s string) *string { return &s }
	tests := map[string]struct {
		Given Given
		When  When
		Then  Then
	}{
		"same schema":                   {Given: Given{configYML: text("config: 0\n")}},
		"missing file":                  {Then: Then{err: "<config>/config.yml is missing: add it with config: 0"}},
		"newer config":                  {Given: Given{configYML: text("config: 1\n")}, Then: Then{err: "this config is schema 1 and this mse reads 0: update mse"}},
		"missing file at a later major": {When: When{engineMajor: 1}, Then: Then{err: "<config>/config.yml is missing: add it with config: 1"}},
		"older config":                  {Given: Given{configYML: text("config: 0\n")}, When: When{engineMajor: 1}, Then: Then{err: "this config is schema 0 and this mse reads 1: migrate the config"}},
		"no config key":                 {Given: Given{configYML: text("other: 1\n")}, Then: Then{err: "<config>/config.yml has no config: version"}},
		"not a number":                  {Given: Given{configYML: text("config: zero\n")}, Then: Then{err: "<config>/config.yml: config must be a whole number"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			config := t.TempDir()
			if tt.Given.configYML != nil {
				require.NoError(t, os.WriteFile(filepath.Join(config, "config.yml"), []byte(*tt.Given.configYML), 0o644))
			}

			err := (&Installation{Config: config}).CheckSchema(tt.When.engineMajor)

			if tt.Then.err == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, replaceAll(tt.Then.err, "<config>", config))
			}
		})
	}
}

func TestANewerSchemaIsMarked(t *testing.T) {
	config := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(config, "config.yml"), []byte("config: 2\n"), 0o644))
	i := &Installation{Config: config}

	assert.ErrorIs(t, i.CheckSchema(1), ErrNewerSchema)
	require.NoError(t, os.WriteFile(filepath.Join(config, "config.yml"), []byte("config: 0\n"), 0o644))
	assert.NotErrorIs(t, i.CheckSchema(1), ErrNewerSchema)
}
