package main

import (
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

func at(t *testing.T, document any, keys ...any) any {
	t.Helper()
	for _, key := range keys {
		switch node := document.(type) {
		case map[string]any:
			document = node[key.(string)]
		case []any:
			document = node[key.(int)]
		default:
			t.Fatalf("no %v in %v", key, document)
		}
	}
	return document
}

func TestTheDelugeClientDeclaresEveryFieldConfigarrChecks(t *testing.T) {
	config := readYAML(t, "config-template/configarr/config.yml")
	for app, imported := range map[string]string{"sonarr": "tv_imported_category", "radarr": "movie_imported_category"} {
		fields := at(t, config, app, "main", "download_clients", "data", 0, "fields").(map[string]any)
		for _, name := range []string{"url_base", imported, "download_directory", "completed_directory"} {
			assert.Contains(t, fields, name, app)
		}
	}
}

func TestTheTemplatesRenovateLooksForImagesInTheImagesFiles(t *testing.T) {
	patterns := readRenovate(t, "config-template/renovate.json").DockerCompose["managerFilePatterns"]
	for _, file := range []string{"images.yml", "images.monitoring.yml"} {
		assert.True(t, matchesAny(t, patterns, file), file)
	}
}

func TestEveryMediaAndDownloadPathTheTemplateDeclaresIsUnderData(t *testing.T) {
	apps := readYAML(t, "config-template/apps.yml")
	configarr := readYAML(t, "config-template/configarr/config.yml")
	var paths []string
	for _, library := range at(t, apps, "jellyfin", "libraries").([]any) {
		paths = append(paths, library.(map[string]any)["path"].(string))
	}
	paths = append(paths,
		at(t, apps, "seerr", "sonarr", "root_folder").(string),
		at(t, apps, "seerr", "radarr", "root_folder").(string),
		at(t, apps, "deluge", "core", "download_location").(string))
	for _, kind := range []string{"sonarr", "radarr"} {
		for _, instance := range at(t, configarr, kind).(map[string]any) {
			if folders, ok := instance.(map[string]any)["root_folders"].([]any); ok {
				for _, folder := range folders {
					paths = append(paths, folder.(string))
				}
			}
		}
	}

	for _, path := range paths {
		assert.True(t, strings.HasPrefix(path, "/data/"), path)
	}
	assert.Subset(t, paths, []string{"/data/media/tvshows", "/data/media/movies", "/data/downloads"})
}
