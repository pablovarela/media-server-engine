package images

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/installation"
)

func TestPinned(t *testing.T) {
	config := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(config, "images.yml"), []byte("services:\n  sonarr:\n    image: lscr.io/linuxserver/sonarr:4.0@sha256:aaa\n  plain:\n    image: busybox:latest\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(config, "images.monitoring.yml"), []byte("services:\n  prometheus:\n    image: registry:5000/prom/prometheus:v3@sha256:bbb\n"), 0o644))

	assert.Equal(t, map[string]bool{
		"lscr.io/linuxserver/sonarr@sha256:aaa":    true,
		"registry:5000/prom/prometheus@sha256:bbb": true,
	}, Pinned(&installation.Installation{Config: config}))
}

func TestOutdated(t *testing.T) {
	pinned := map[string]bool{"lscr.io/linuxserver/sonarr@sha256:aaa": true, "registry:5000/prom/prometheus@sha256:bbb": true}
	type Given struct {
		local []image.Summary
	}
	type Then struct {
		outdated []string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"the pinned image stays": {
			Given: Given{local: []image.Summary{{RepoTags: []string{"lscr.io/linuxserver/sonarr:4.0"}, RepoDigests: []string{"lscr.io/linuxserver/sonarr@sha256:aaa"}}}},
		},
		"an older tag goes by tag": {
			Given: Given{local: []image.Summary{{RepoTags: []string{"lscr.io/linuxserver/sonarr:3.9"}, RepoDigests: []string{"lscr.io/linuxserver/sonarr@sha256:old"}}}},
			Then:  Then{outdated: []string{"lscr.io/linuxserver/sonarr:3.9"}},
		},
		"an untagged one goes by digest": {
			Given: Given{local: []image.Summary{{RepoDigests: []string{"lscr.io/linuxserver/sonarr@sha256:older"}}}},
			Then:  Then{outdated: []string{"lscr.io/linuxserver/sonarr@sha256:older"}},
		},
		"other repositories are left alone": {
			Given: Given{local: []image.Summary{{RepoTags: []string{"busybox:latest"}, RepoDigests: []string{"busybox@sha256:x"}}}},
		},
		"registry with a port": {
			Given: Given{local: []image.Summary{{RepoTags: []string{"registry:5000/prom/prometheus:v2"}, RepoDigests: []string{"registry:5000/prom/prometheus@sha256:old"}}}},
			Then:  Then{outdated: []string{"registry:5000/prom/prometheus:v2"}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.Then.outdated, Outdated(pinned, tt.Given.local))
		})
	}
}

func TestPrune(t *testing.T) {
	config := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(config, "images.yml"), []byte("services:\n  sonarr:\n    image: lscr.io/linuxserver/sonarr:4.0@sha256:aaa\n"), 0o644))
	docker := newMockClient(t)
	docker.EXPECT().ImageList(mock.Anything, client.ImageListOptions{All: true}).Return(client.ImageListResult{Items: []image.Summary{
		{RepoTags: []string{"lscr.io/linuxserver/sonarr:3.9"}, RepoDigests: []string{"lscr.io/linuxserver/sonarr@sha256:old"}},
		{RepoDigests: []string{"lscr.io/linuxserver/sonarr@sha256:older"}},
	}}, nil)
	docker.EXPECT().ImageRemove(mock.Anything, "lscr.io/linuxserver/sonarr:3.9", client.ImageRemoveOptions{}).Return(client.ImageRemoveResult{}, nil)
	docker.EXPECT().ImageRemove(mock.Anything, "lscr.io/linuxserver/sonarr@sha256:older", client.ImageRemoveOptions{}).Return(client.ImageRemoveResult{}, errors.New("in use"))
	var out bytes.Buffer

	require.NoError(t, Prune(context.Background(), docker, &installation.Installation{Config: config}, &out))

	assert.Equal(t, "Checking local images against the 1 pinned in the config...\nremoved lscr.io/linuxserver/sonarr:3.9\nkept lscr.io/linuxserver/sonarr@sha256:older (still in use)\nRemoved 1 outdated image, kept 1 still in use.\n", out.String())
}

func TestPruneWithNothingOutdated(t *testing.T) {
	config := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(config, "images.yml"), []byte("services:\n  sonarr:\n    image: lscr.io/linuxserver/sonarr:4.0@sha256:aaa\n  radarr:\n    image: lscr.io/linuxserver/radarr:5.0@sha256:bbb\n"), 0o644))
	docker := newMockClient(t)
	docker.EXPECT().ImageList(mock.Anything, client.ImageListOptions{All: true}).Return(client.ImageListResult{Items: []image.Summary{
		{RepoTags: []string{"lscr.io/linuxserver/sonarr:4.0"}, RepoDigests: []string{"lscr.io/linuxserver/sonarr@sha256:aaa"}},
	}}, nil)
	var out bytes.Buffer

	require.NoError(t, Prune(context.Background(), docker, &installation.Installation{Config: config}, &out))

	assert.Equal(t, "Checking local images against the 2 pinned in the config...\nNo outdated images.\n", out.String())
}
