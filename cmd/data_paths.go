package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

var filesDeclaringPaths = []string{"apps.yml", filepath.Join("configarr", "config.yml")}

func declaredDataPaths(config string) []string {
	var paths []string
	for _, file := range filesDeclaringPaths {
		text, err := os.ReadFile(filepath.Join(config, file)) //nolint:gosec // the installation's own config files
		if err != nil {
			continue
		}
		var document yaml.Node
		if yaml.Unmarshal(text, &document) == nil {
			paths = append(paths, pathsUnderData(&document)...)
		}
	}
	return paths
}

func pathsUnderData(node *yaml.Node) []string {
	if node.Kind == yaml.ScalarNode {
		if strings.HasPrefix(node.Value, "/data/") {
			return []string{node.Value}
		}
		return nil
	}
	var paths []string
	for _, child := range node.Content {
		paths = append(paths, pathsUnderData(child)...)
	}
	return paths
}
