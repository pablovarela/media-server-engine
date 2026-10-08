package main

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type renovateConfig struct {
	Extends           []string            `json:"extends"`
	PostUpdateOptions []string            `json:"postUpdateOptions"`
	PackageRules      []map[string]any    `json:"packageRules"`
	CustomManagers    []renovateManager   `json:"customManagers"`
	DockerCompose     map[string][]string `json:"docker-compose"`
}

type renovateManager struct {
	FilePatterns []string `json:"managerFilePatterns"`
	MatchStrings []string `json:"matchStrings"`
	Dependency   string   `json:"depNameTemplate"`
}

func readRenovate(t *testing.T, path string) renovateConfig {
	t.Helper()
	text, err := os.ReadFile(path)
	require.NoError(t, err)
	var config renovateConfig
	require.NoError(t, json.Unmarshal(text, &config))
	return config
}

func renovatePattern(t *testing.T, pattern string) *regexp.Regexp {
	t.Helper()
	compiled, err := regexp.Compile(strings.ReplaceAll(strings.Trim(pattern, "/"), `\/`, "/"))
	require.NoError(t, err)
	return compiled
}

func matchesAny(t *testing.T, patterns []string, file string) bool {
	t.Helper()
	for _, pattern := range patterns {
		if renovatePattern(t, pattern).MatchString(file) {
			return true
		}
	}
	return false
}

func currentValue(t *testing.T, matchString, text string) string {
	t.Helper()
	pattern, err := regexp.Compile(matchString)
	require.NoError(t, err)
	group := pattern.SubexpIndex("currentValue")
	require.GreaterOrEqual(t, group, 0, "%s has no currentValue group", matchString)
	if found := pattern.FindStringSubmatch(text); found != nil {
		return found[group]
	}
	return ""
}

func managersOf(config renovateConfig, dependency string) []renovateManager {
	var managers []renovateManager
	for _, manager := range config.CustomManagers {
		if manager.Dependency == dependency {
			managers = append(managers, manager)
		}
	}
	return managers
}

func findsTheVersionIn(dependency, file, version string) func(t *testing.T, engine, _ renovateConfig) {
	return func(t *testing.T, engine, _ renovateConfig) {
		managers := managersOf(engine, dependency)
		require.Len(t, managers, 1)
		require.True(t, matchesAny(t, managers[0].FilePatterns, file))
		text, err := os.ReadFile(file)
		require.NoError(t, err)
		require.NotEmpty(t, managers[0].MatchStrings)
		for _, matchString := range managers[0].MatchStrings {
			assert.Regexp(t, version, currentValue(t, matchString, string(text)))
		}
	}
}

func theEngines(engine, _ renovateConfig) renovateConfig { return engine }

func theTemplates(_, template renovateConfig) renovateConfig { return template }

func updatesTheImagesIn(file string, of func(engine, template renovateConfig) renovateConfig) func(t *testing.T, engine, template renovateConfig) {
	return func(t *testing.T, engine, template renovateConfig) {
		assert.True(t, matchesAny(t, of(engine, template).DockerCompose["managerFilePatterns"], file))
	}
}

func TestTheRenovateConfigs(t *testing.T) {
	tests := map[string]struct {
		Then func(t *testing.T, engine, template renovateConfig)
	}{
		"the engine's keeps action digests pinned": {
			Then: func(t *testing.T, engine, _ renovateConfig) {
				assert.Contains(t, engine.Extends, "helpers:pinGitHubActionDigests")
			},
		},
		"the engine's tidies the Go modules": {
			Then: func(t *testing.T, engine, _ renovateConfig) {
				assert.Contains(t, engine.PostUpdateOptions, "gomodTidy")
			},
		},
		"the engine's updates the template's stack images": {
			Then: updatesTheImagesIn("config-template/images.yml", theEngines),
		},
		"the engine's leaves its compose files to the template's images": {
			Then: func(t *testing.T, engine, _ renovateConfig) {
				assert.False(t, matchesAny(t, engine.DockerCompose["managerFilePatterns"], "docker-compose.yml"))
			},
		},
		"the template's updates an installation's stack images": {
			Then: updatesTheImagesIn("images.yml", theTemplates),
		},
		"the template's image versioning rules are the engine's too": {
			Then: func(t *testing.T, engine, template renovateConfig) {
				versioning := 0
				for _, rule := range template.PackageRules {
					if _, found := rule["versioning"]; found {
						versioning++
						assert.Contains(t, engine.PackageRules, rule)
					}
				}
				assert.NotZero(t, versioning)
			},
		},
		"the template's merges minor, patch and digest updates itself, never majors": {
			Then: func(t *testing.T, _, template renovateConfig) {
				assert.Contains(t, template.PackageRules, map[string]any{
					"matchUpdateTypes": []any{"minor", "patch", "digest", "pinDigest"},
					"automerge":        true, "automergeType": "pr", "platformAutomerge": false,
				})
				assert.Contains(t, template.Extends, "helpers:pinGitHubActionDigests")
			},
		},
		"no waiting period": {
			Then: func(t *testing.T, engine, template renovateConfig) {
				for _, config := range []renovateConfig{engine, template} {
					for _, rule := range config.PackageRules {
						assert.NotContains(t, rule, "minimumReleaseAge")
					}
				}
			},
		},
		"the engine's finds restic's version in mse's pin file": {
			Then: findsTheVersionIn("restic/restic", "internal/restic/release.env", `^\d+\.\d+\.\d+$`),
		},
		"the engine's finds shellcheck's version in the CI workflow": {
			Then: findsTheVersionIn("koalaman/shellcheck", ".github/workflows/test.yml", `^v\d+\.\d+\.\d+$`),
		},
		"the engine's tracks restic in mse's pin file only": {
			Then: func(t *testing.T, engine, _ renovateConfig) {
				restic := managersOf(engine, "restic/restic")
				require.Len(t, restic, 1)
				assert.Equal(t, []string{`/^internal\/restic\/release\.env$/`}, restic[0].FilePatterns)
			},
		},
		"the engine's tracks only the tools mse uses": {
			Then: func(t *testing.T, engine, _ renovateConfig) {
				var dependencies []string
				for _, manager := range engine.CustomManagers {
					dependencies = append(dependencies, manager.Dependency)
				}
				assert.ElementsMatch(t, []string{"restic/restic", "koalaman/shellcheck"}, dependencies)
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			engine := readRenovate(t, "renovate.json")
			template := readRenovate(t, "config-template/renovate.json")

			tt.Then(t, engine, template)
		})
	}
}
