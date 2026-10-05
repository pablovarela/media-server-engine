package cmd

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/pablovarela/media-server-engine/internal/github"
	"github.com/pablovarela/media-server-engine/internal/selfupdate"
	"github.com/pablovarela/media-server-engine/internal/version"
)

func TestUpdateCommand(t *testing.T) {
	build := version.Build{Version: "v0.8.0", Commit: "1a2b3c4", Date: "2026-10-05"}
	major := &github.Release{Tag: "v1.0.0", URL: "https://github.com/pablovarela/media-server-engine/releases/tag/v1.0.0"}
	available := "v1.0.0 is available and needs config changes (release notes: https://github.com/pablovarela/media-server-engine/releases/tag/v1.0.0)\nInstall it with: mse update --force\n"
	checking := "No installation here; updating mse only.\nCurrent version: v0.8.0\nChecking for a newer compatible release...\n"

	type Given struct {
		updating string
		result   selfupdate.Result
		err      error
		checks   bool
	}
	type When struct {
		args  []string
		force bool
	}
	type Then struct {
		code   int
		stdout string
		stderr string
	}
	tests := map[string]struct {
		Given Given
		When  When
		Then  Then
	}{
		"updated": {
			Given: Given{checks: true, updating: "v0.9.0", result: selfupdate.Result{From: "v0.8.0", To: "v0.9.0"}},
			When:  When{args: []string{"update"}},
			Then:  Then{stdout: checking + "Updating to v0.9.0...\nSuccessfully updated from v0.8.0 to v0.9.0\n"},
		},
		"up to date": {
			Given: Given{checks: true, result: selfupdate.Result{From: "v0.8.0"}},
			When:  When{args: []string{"update"}},
			Then:  Then{stdout: checking + "mse is up to date (v0.8.0)\n"},
		},
		"up to date with a newer major": {
			Given: Given{checks: true, result: selfupdate.Result{From: "v0.8.0", NewerMajor: major}},
			When:  When{args: []string{"update"}},
			Then:  Then{stdout: checking + "mse is up to date (v0.8.0)\n" + available},
		},
		"updated with a newer major": {
			Given: Given{checks: true, updating: "v0.9.0", result: selfupdate.Result{From: "v0.8.0", To: "v0.9.0", NewerMajor: major}},
			When:  When{args: []string{"update"}},
			Then:  Then{stdout: checking + "Updating to v0.9.0...\nSuccessfully updated from v0.8.0 to v0.9.0\n" + available},
		},
		"force": {
			Given: Given{checks: true, updating: "v1.0.0", result: selfupdate.Result{From: "v0.8.0", To: "v1.0.0"}},
			When:  When{args: []string{"update", "--force"}, force: true},
			Then:  Then{stdout: "No installation here; updating mse only.\nCurrent version: v0.8.0\nChecking for a newer release, including ones that need config changes...\nUpdating to v1.0.0...\nSuccessfully updated from v0.8.0 to v1.0.0\n"},
		},
		"dev build": {
			Given: Given{err: selfupdate.ErrDevBuild},
			When:  When{args: []string{"update"}},
			Then:  Then{stdout: "No installation here; updating mse only.\nUpdating mse... dev build, not updated.\n"},
		},
		"failure while updating": {
			Given: Given{checks: true, updating: "v0.9.0", err: errors.New("release v0.9.0: checksum mismatch for mse_linux_arm64.tar.gz")},
			When:  When{args: []string{"update"}},
			Then:  Then{code: 1, stdout: checking + "Updating to v0.9.0...\n", stderr: "mse: release v0.9.0: checksum mismatch for mse_linux_arm64.tar.gz\n"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			update := newMockUpdater(t)
			update.EXPECT().Update(mock.Anything, build, tt.When.force, mock.Anything).RunAndReturn(
				func(_ context.Context, b version.Build, force bool, p selfupdate.Progress) (selfupdate.Result, error) {
					if tt.Given.checks {
						p.Checking(b.Version, force)
					}
					if tt.Given.updating != "" {
						p.Updating(tt.Given.updating)
					}
					return tt.Given.result, tt.Given.err
				})
			root := NewRootCommand(Dependencies{Build: build, Update: update, Home: t.TempDir(), Environment: func(string) string { return "" }})
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
