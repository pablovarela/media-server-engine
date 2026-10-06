package main

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheLicense(t *testing.T) {
	tests := map[string]struct {
		Then func(t *testing.T, license string, goreleaser map[string]any)
	}{
		"the repository is MIT licensed": {
			Then: func(t *testing.T, license string, _ map[string]any) {
				assert.True(t, strings.HasPrefix(license, "MIT License\n"))
				assert.Contains(t, license, "Copyright (c) 2026 Pablo Varela")
			},
		},
		"every release archive carries the license next to mse": {
			Then: func(t *testing.T, _ string, goreleaser map[string]any) {
				archives := at[[]any](t, goreleaser, "archives")
				require.NotEmpty(t, archives)
				for n := range archives {
					assert.Contains(t, at[[]any](t, goreleaser, "archives", n, "files"), "LICENSE")
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			license, err := os.ReadFile("LICENSE")
			require.NoError(t, err)
			goreleaser := readYAML(t, ".goreleaser.yaml")

			tt.Then(t, string(license), goreleaser)
		})
	}
}
