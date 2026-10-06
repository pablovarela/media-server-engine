package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func readYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	text, err := os.ReadFile(path)
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, yaml.Unmarshal(text, &document))
	return document
}

func as[T any](t *testing.T, value any, where string) T {
	t.Helper()
	typed, ok := value.(T)
	require.True(t, ok, "%s is %T, not %T", where, value, typed)
	return typed
}

func at[T any](t *testing.T, document any, keys ...any) T {
	t.Helper()
	for n, key := range keys {
		path := fmt.Sprintf("%v", keys[:n+1])
		switch node := document.(type) {
		case map[string]any:
			field := as[string](t, key, path)
			require.Contains(t, node, field, path)
			document = node[field]
		case []any:
			index := as[int](t, key, path)
			require.Less(t, index, len(node), path)
			document = node[index]
		default:
			require.Failf(t, "not a map or a list", "%v is %T", keys[:n], document)
		}
	}
	return as[T](t, document, fmt.Sprintf("%v", keys))
}

func declaresEveryFieldConfigarrChecks(app, importedCategory string) func(t *testing.T, _, configarr map[string]any) {
	return func(t *testing.T, _, configarr map[string]any) {
		fields := at[map[string]any](t, configarr, app, "main", "download_clients", "data", 0, "fields")
		for _, name := range []string{"url_base", importedCategory, "download_directory", "completed_directory"} {
			assert.Contains(t, fields, name)
		}
	}
}

func mediaAndDownloadPaths(t *testing.T, apps, configarr map[string]any) []string {
	t.Helper()
	var paths []string
	for _, library := range at[[]any](t, apps, "jellyfin", "libraries") {
		paths = append(paths, at[string](t, library, "path"))
	}
	paths = append(paths,
		at[string](t, apps, "seerr", "sonarr", "root_folder"),
		at[string](t, apps, "seerr", "radarr", "root_folder"),
		at[string](t, apps, "deluge", "core", "download_location"))
	for _, kind := range []string{"sonarr", "radarr"} {
		for name, instance := range at[map[string]any](t, configarr, kind) {
			if _, declared := as[map[string]any](t, instance, kind+"."+name)["root_folders"]; declared {
				for _, folder := range at[[]any](t, instance, "root_folders") {
					paths = append(paths, as[string](t, folder, kind+"."+name+".root_folders"))
				}
			}
		}
	}
	return paths
}

func TestTheConfigTemplate(t *testing.T) {
	tests := map[string]struct {
		Then func(t *testing.T, apps, configarr map[string]any)
	}{
		"sonarr's deluge client declares every field configarr checks": {
			Then: declaresEveryFieldConfigarrChecks("sonarr", "tv_imported_category"),
		},
		"radarr's deluge client declares every field configarr checks": {
			Then: declaresEveryFieldConfigarrChecks("radarr", "movie_imported_category"),
		},
		"every media and download path is under /data": {
			Then: func(t *testing.T, apps, configarr map[string]any) {
				paths := mediaAndDownloadPaths(t, apps, configarr)

				for _, path := range paths {
					assert.True(t, strings.HasPrefix(path, "/data/"), path)
				}
				assert.Subset(t, paths, []string{"/data/media/tvshows", "/data/media/movies", "/data/downloads"})
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			apps := readYAML(t, "config-template/apps.yml")
			configarr := readYAML(t, "config-template/configarr/config.yml")

			tt.Then(t, apps, configarr)
		})
	}
}
