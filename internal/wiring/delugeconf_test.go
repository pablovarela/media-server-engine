package wiring

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestADelugeConfIsReadAsItsHeaderAndBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.conf")
	require.NoError(t, os.WriteFile(path, []byte("{\n    \"file\": 1,\n    \"format\": 1\n}{\n    \"max_upload_speed\": -1.0,\n    \"enabled_plugins\": []\n}"), 0o644))

	conf, err := readDelugeConf(path, pluginConfHeader)

	require.NoError(t, err)
	assert.Equal(t, "{\n    \"file\": 1,\n    \"format\": 1\n}", string(conf.header))
	assert.Equal(t, "-1.0", show(conf.body["max_upload_speed"]))
	assert.False(t, conf.dirty)
}

func TestAMissingDelugeConfStartsFromItsDefaultHeader(t *testing.T) {
	conf, err := readDelugeConf(filepath.Join(t.TempDir(), "web.conf"), webConfHeader)

	require.NoError(t, err)
	assert.Equal(t, webConfHeader, string(conf.header))
	assert.Empty(t, conf.body)
}

func TestADelugeConfIsWrittenAsPythonsJSONWithItsFloatsKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.conf")
	require.NoError(t, os.WriteFile(path, []byte(`{"file": 1, "format": 1}{"max_upload_speed": -1.0, "download_location": "/downloads", "enabled_plugins": []}`), 0o644))
	conf, err := readDelugeConf(path, pluginConfHeader)
	require.NoError(t, err)
	conf.set("max_upload_speed", pythonValue(2000.0))
	conf.set("enabled_plugins", []any{"Label"})
	conf.set("max_connections_global", pythonValue(400))

	require.NoError(t, conf.save())

	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, `{"file": 1, "format": 1}{
    "download_location": "/downloads",
    "enabled_plugins": [
        "Label"
    ],
    "max_connections_global": 400,
    "max_upload_speed": 2000.0
}`, string(written))
}

func TestNumbersCompareByValueWhateverTheirSpelling(t *testing.T) {
	conf := &delugeConf{body: map[string]any{}}
	conf.body["min"] = pythonNumber("168.0")

	assert.True(t, same(conf.body["min"], pythonValue(168.0)))
	assert.True(t, same(pythonNumber("500"), pythonValue(500.0)))
	assert.False(t, same(pythonNumber("500.0"), pythonValue(2000.0)))
	assert.Equal(t, "500.0", show(pythonNumber("500.0")))
	assert.Equal(t, "2000.0", show(pythonValue(2000.0)))
	assert.Equal(t, "400", show(pythonValue(400)))
}
