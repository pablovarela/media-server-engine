package cmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

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
