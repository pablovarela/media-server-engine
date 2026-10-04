package version

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildLine(t *testing.T) {
	type Given struct {
		stamped   Build
		buildInfo *debug.BuildInfo
	}
	type Then struct {
		line string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"release build stamped by goreleaser": {
			Given: Given{
				stamped:   Build{Version: "v0.7.0", Commit: "1a2b3c4", Date: "2026-10-05"},
				buildInfo: withVCS("9f8e7d6c5b4a3210", "false"),
			},
			Then: Then{line: "mse v0.7.0 (commit 1a2b3c4, built 2026-10-05)"},
		},
		"dev build from a clean checkout": {
			Given: Given{stamped: Build{Version: "dev"}, buildInfo: withVCS("9f8e7d6c5b4a3210", "false")},
			Then:  Then{line: "mse dev (commit 9f8e7d6)"},
		},
		"dev build with uncommitted changes": {
			Given: Given{stamped: Build{Version: "dev"}, buildInfo: withVCS("9f8e7d6c5b4a3210", "true")},
			Then:  Then{line: "mse dev (commit 9f8e7d6-dirty)"},
		},
		"dev build outside a git checkout": {
			Given: Given{stamped: Build{Version: "dev"}, buildInfo: &debug.BuildInfo{}},
			Then:  Then{line: "mse dev"},
		},
		"binary without build information": {
			Given: Given{stamped: Build{Version: "dev"}},
			Then:  Then{line: "mse dev"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			readBuildInfo := func() (*debug.BuildInfo, bool) {
				return tt.Given.buildInfo, tt.Given.buildInfo != nil
			}

			assert.Equal(t, tt.Then.line, resolve(tt.Given.stamped, readBuildInfo).String())
		})
	}
}

func withVCS(revision, modified string) *debug.BuildInfo {
	return &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: revision},
		{Key: "vcs.modified", Value: modified},
	}}
}
