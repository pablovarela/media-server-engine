package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/paint"
	"github.com/pablovarela/media-server-engine/internal/version"
)

func TestRun(t *testing.T) {
	build := version.Build{Version: "v0.7.0", Commit: "1a2b3c4", Date: "2026-10-05"}
	versionLine := "mse v0.7.0 (commit 1a2b3c4, built 2026-10-05)\n"

	type When struct {
		args []string
	}
	type Then struct {
		code   int
		stdout string
		stderr string
	}
	tests := map[string]struct {
		When When
		Then Then
	}{
		"version command": {
			When: When{args: []string{"version"}},
			Then: Then{code: 0, stdout: versionLine},
		},
		"version flag": {
			When: When{args: []string{"--version"}},
			Then: Then{code: 0, stdout: versionLine},
		},
		"unknown command": {
			When: When{args: []string{"nope"}},
			Then: Then{code: 1, stderr: "mse: unknown command \"nope\" for \"mse\"\n"},
		},
		"version takes no arguments": {
			When: When{args: []string{"version", "extra"}},
			Then: Then{code: 1, stderr: "mse: unknown command \"extra\" for \"mse version\"\n"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			root := NewRootCommand(Dependencies{Build: build, Update: newMockUpdater(t)})
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			code := run(context.Background(), root, tt.When.args)

			assert.Equal(t, tt.Then.code, code)
			assert.Equal(t, tt.Then.stdout, stdout.String())
			assert.Equal(t, tt.Then.stderr, stderr.String())
		})
	}
}

func TestRunPaintsTheErrorRed(t *testing.T) {
	paint.Enable(true)
	t.Cleanup(func() { paint.Enable(false) })
	root := NewRootCommand(Dependencies{Update: newMockUpdater(t)})
	var stderr bytes.Buffer
	root.SetErr(&stderr)

	run(context.Background(), root, []string{"nope"})

	assert.Equal(t, "\x1b[31mmse: unknown command \"nope\" for \"mse\"\x1b[0m\n", stderr.String())
}

func TestEveryRunIsLogged(t *testing.T) {
	getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
	root := NewRootCommand(Dependencies{Environment: getenv, Home: home, Update: newMockUpdater(t), Host: installation.Host{GOOS: "linux", Hostname: func() (string, error) { return "gorgon", nil }}})
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&bytes.Buffer{})

	code := run(context.Background(), root, []string{"urls"})

	assert.Equal(t, 0, code)
	logged, err := os.ReadFile(filepath.Join(home, ".local", "state", "mse", "gorgon", "logs", "mse.log"))
	require.NoError(t, err)
	text := string(logged)
	assert.Regexp(t, ` urls\[[0-9a-f]{6}\] start urls\n`, text)
	assert.NotContains(t, text, strings.SplitN(stdout.String(), "\n", 2)[0], "data commands keep their output out of the log")
	assert.Regexp(t, `\] finish exit 0 after [0-9.]+[µm]?s\n$`, text)
}

func TestRunsBeforeAnInstallationLogToTheGlobalLog(t *testing.T) {
	logs := t.TempDir()
	root := NewRootCommand(Dependencies{Build: version.Build{Version: "v0.7.0"}, Update: newMockUpdater(t)})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})

	code := runLogged(context.Background(), root, []string{"version"}, logs)

	assert.Equal(t, 0, code)
	logged, err := os.ReadFile(filepath.Join(logs, "mse.log"))
	require.NoError(t, err)
	assert.Contains(t, string(logged), "] start version\n")
}

func TestTheVerboseFlagExists(t *testing.T) {
	root := NewRootCommand(Dependencies{Update: newMockUpdater(t)})

	assert.NotNil(t, root.PersistentFlags().Lookup("verbose"))
	assert.Equal(t, "v", root.PersistentFlags().Lookup("verbose").Shorthand)
}

func TestRunIDFrom(t *testing.T) {
	assert.Equal(t, "a1b2c3", runIDFrom([]string{"apply", "--after-update=a1b2c3"}))
	assert.Len(t, runIDFrom([]string{"apply", "--after-update=../../x"}), 6)
	assert.NotEqual(t, "a1b2c3", runIDFrom([]string{"apply", "--after-update=../../x"}))
	assert.Len(t, runIDFrom([]string{"update"}), 6)
}
