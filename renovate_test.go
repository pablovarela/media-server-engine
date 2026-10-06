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

func TestRenovateKeepsActionDigestsPinnedAndTidiesGoModules(t *testing.T) {
	config := readRenovate(t, "renovate.json")

	assert.Contains(t, config.Extends, "helpers:pinGitHubActionDigests")
	assert.Contains(t, config.PostUpdateOptions, "gomodTidy")
}

func TestRenovateUpdatesTheTemplatesImagePins(t *testing.T) {
	patterns := readRenovate(t, "renovate.json").DockerCompose["managerFilePatterns"]

	for _, file := range []string{"config-template/images.yml", "config-template/images.monitoring.yml"} {
		assert.True(t, matchesAny(t, patterns, file), file)
	}
	assert.False(t, matchesAny(t, patterns, "docker-compose.yml"))
}

func TestTheTemplatesImageVersioningRulesAreTheEnginesToo(t *testing.T) {
	engine := readRenovate(t, "renovate.json").PackageRules
	for _, rule := range readRenovate(t, "config-template/renovate.json").PackageRules {
		assert.Contains(t, engine, rule)
	}
}

func TestEveryVersionRenovateTracksByRegexIsFoundInItsFile(t *testing.T) {
	expected := map[string]string{
		"restic/restic":       "internal/restic/release.env",
		"koalaman/shellcheck": ".github/workflows/test.yml",
	}
	found := map[string]string{}
	for _, manager := range readRenovate(t, "renovate.json").CustomManagers {
		file, tracked := expected[manager.Dependency]
		if !tracked || !matchesAny(t, manager.FilePatterns, file) {
			continue
		}
		text, err := os.ReadFile(file)
		require.NoError(t, err)
		for _, match := range manager.MatchStrings {
			pattern := regexp.MustCompile(match)
			if groups := pattern.FindStringSubmatch(string(text)); groups != nil {
				found[manager.Dependency] = groups[pattern.SubexpIndex("currentValue")]
			}
		}
	}
	for dependency := range expected {
		assert.NotEmpty(t, found[dependency], dependency)
	}
	assert.Regexp(t, `^\d`, found["restic/restic"])
}

func TestRenovateTracksResticInMsesPinFileOnly(t *testing.T) {
	var restic []renovateManager
	for _, manager := range readRenovate(t, "renovate.json").CustomManagers {
		if manager.Dependency == "restic/restic" {
			restic = append(restic, manager)
		}
	}

	require.Len(t, restic, 1)
	assert.Equal(t, []string{`/^internal\/restic\/release\.env$/`}, restic[0].FilePatterns)
}

func TestRenovateTracksOnlyTheToolsMseUses(t *testing.T) {
	var dependencies []string
	for _, manager := range readRenovate(t, "renovate.json").CustomManagers {
		dependencies = append(dependencies, manager.Dependency)
	}

	assert.ElementsMatch(t, []string{"restic/restic", "koalaman/shellcheck"}, dependencies)
}
