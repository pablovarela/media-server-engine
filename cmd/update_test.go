package cmd

import (
	"bytes"
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
	available := "v1.0.0 is available and may need config changes: mse update --force (release notes: https://github.com/pablovarela/media-server-engine/releases/tag/v1.0.0)\n"

	type Given struct {
		result selfupdate.Result
		err    error
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
			Given: Given{result: selfupdate.Result{From: "v0.8.0", To: "v0.9.0"}},
			When:  When{args: []string{"update"}},
			Then:  Then{stdout: "updated mse from v0.8.0 to v0.9.0\n"},
		},
		"up to date": {
			Given: Given{result: selfupdate.Result{From: "v0.8.0"}},
			When:  When{args: []string{"update"}},
			Then:  Then{stdout: "mse v0.8.0 is up to date\n"},
		},
		"up to date with a newer major": {
			Given: Given{result: selfupdate.Result{From: "v0.8.0", NewerMajor: major}},
			When:  When{args: []string{"update"}},
			Then:  Then{stdout: "mse v0.8.0 is up to date\n" + available},
		},
		"updated with a newer major": {
			Given: Given{result: selfupdate.Result{From: "v0.8.0", To: "v0.9.0", NewerMajor: major}},
			When:  When{args: []string{"update"}},
			Then:  Then{stdout: "updated mse from v0.8.0 to v0.9.0\n" + available},
		},
		"force": {
			Given: Given{result: selfupdate.Result{From: "v0.8.0", To: "v1.0.0"}},
			When:  When{args: []string{"update", "--force"}, force: true},
			Then:  Then{stdout: "updated mse from v0.8.0 to v1.0.0\n"},
		},
		"dev build": {
			Given: Given{err: selfupdate.ErrDevBuild},
			When:  When{args: []string{"update"}},
			Then:  Then{code: 1, stderr: "mse: dev builds don't update themselves; install a release with install.sh\n"},
		},
		"failure": {
			Given: Given{err: errors.New("release v0.9.0: checksum mismatch for mse_linux_arm64.tar.gz")},
			When:  When{args: []string{"update"}},
			Then:  Then{code: 1, stderr: "mse: release v0.9.0: checksum mismatch for mse_linux_arm64.tar.gz\n"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			update := newMockUpdater(t)
			update.EXPECT().Update(mock.Anything, build, tt.When.force).Return(tt.Given.result, tt.Given.err)
			root := NewRootCommand(build, update)
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			code := run(root, tt.When.args)

			assert.Equal(t, tt.Then.code, code)
			assert.Equal(t, tt.Then.stdout, stdout.String())
			assert.Equal(t, tt.Then.stderr, stderr.String())
		})
	}
}
