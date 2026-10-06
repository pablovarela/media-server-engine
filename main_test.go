package main

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngineEmbedsTheTemplateWithDotfiles(t *testing.T) {
	for _, file := range []string{"config-template/.gitignore", "config-template/config.yml", "config-template/apps.yml", "config-template/configarr/config.yml"} {
		_, err := fs.Stat(engine, file)
		require.NoError(t, err, file)
	}
	_, err := fs.Stat(engine, "config-template/installation.env")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}
