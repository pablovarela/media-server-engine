package homepage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"

	"go.yaml.in/yaml/v3"
)

var healthcheckMarker = regexp.MustCompile(`@HEALTHCHECK_([A-Z]+)@`)

func renderServices(ctx context.Context, text string, slugFor func(job string) string, key string, checks CheckLister, warn io.Writer) (string, error) {
	groups, err := sequence(text)
	if err != nil {
		return "", fmt.Errorf("services.yaml: %w", err)
	}
	switch {
	case key == "":
		groups.Content = withoutHealthchecks(groups.Content)
	case healthcheckMarker.MatchString(text):
		existing, err := checks.Slugs(ctx, key)
		if err != nil {
			_, _ = fmt.Fprintf(warn, "could not read the checks from healthchecks (%v); the page is drawn without them\n", err)
			existing = map[string]bool{}
		}
		groups.Content = withChecks(groups.Content, existing, slugFor)
	}
	return encode(groups)
}

func renderWidgets(text string) (string, error) {
	widgets, err := sequence(text)
	if err != nil {
		return "", fmt.Errorf("widgets.yaml: %w", err)
	}
	for _, widget := range widgets.Content {
		if _, options, ok := entry(widget); ok && options.Kind == yaml.MappingNode && value(options, "href") == "" && hasKey(options, "href") {
			removeKeys(options, "href", "target")
		}
	}
	return encode(widgets)
}

func sequence(text string) (*yaml.Node, error) {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(text), &document); err != nil {
		return nil, err
	}
	if len(document.Content) == 0 || document.Content[0].Tag == "!!null" {
		return &yaml.Node{Kind: yaml.SequenceNode}, nil
	}
	root := document.Content[0]
	if root.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("expected a list")
	}
	return root, nil
}

func encode(node *yaml.Node) (string, error) {
	if len(node.Content) == 0 {
		return "[]\n", nil
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(node); err != nil {
		return "", err
	}
	return out.String(), encoder.Close()
}

func entry(node *yaml.Node) (string, *yaml.Node, bool) {
	if node.Kind != yaml.MappingNode || len(node.Content) < 2 {
		return "", nil, false
	}
	return node.Content[0].Value, node.Content[1], true
}

func withoutHealthchecks(items []*yaml.Node) []*yaml.Node {
	var kept []*yaml.Node
	for _, item := range items {
		name, value, ok := entry(item)
		if ok && name == "Healthchecks" {
			continue
		}
		if ok && value.Kind == yaml.SequenceNode {
			value.Content = withoutHealthchecks(value.Content)
		}
		kept = append(kept, item)
	}
	return kept
}

func withChecks(items []*yaml.Node, existing map[string]bool, slugFor func(string) string) []*yaml.Node {
	var kept []*yaml.Node
	for _, item := range items {
		_, value, ok := entry(item)
		if ok && value.Kind == yaml.SequenceNode {
			found := withChecks(value.Content, existing, slugFor)
			if len(value.Content) > 0 && len(found) == 0 {
				continue
			}
			value.Content = found
		}
		if ok && value.Kind == yaml.MappingNode && !filledWithChecks(value, existing, slugFor) {
			continue
		}
		kept = append(kept, item)
	}
	return kept
}

func filledWithChecks(tile *yaml.Node, existing map[string]bool, slugFor func(string) string) bool {
	scalars := scalarsIn(tile)
	for _, scalar := range scalars {
		for _, match := range healthcheckMarker.FindAllStringSubmatch(scalar.Value, -1) {
			if !existing[slugFor(match[1])] {
				return false
			}
		}
	}
	for _, scalar := range scalars {
		scalar.Value = healthcheckMarker.ReplaceAllStringFunc(scalar.Value, func(marker string) string {
			return slugFor(healthcheckMarker.FindStringSubmatch(marker)[1])
		})
	}
	return true
}

func scalarsIn(node *yaml.Node) []*yaml.Node {
	if node.Kind == yaml.ScalarNode {
		return []*yaml.Node{node}
	}
	var scalars []*yaml.Node
	for _, child := range node.Content {
		scalars = append(scalars, scalarsIn(child)...)
	}
	return scalars
}

func hasKey(mapping *yaml.Node, key string) bool {
	for n := 0; n+1 < len(mapping.Content); n += 2 {
		if mapping.Content[n].Value == key {
			return true
		}
	}
	return false
}

func value(mapping *yaml.Node, key string) string {
	for n := 0; n+1 < len(mapping.Content); n += 2 {
		if mapping.Content[n].Value == key {
			return mapping.Content[n+1].Value
		}
	}
	return ""
}

func removeKeys(mapping *yaml.Node, keys ...string) {
	var kept []*yaml.Node
	for n := 0; n+1 < len(mapping.Content); n += 2 {
		drop := false
		for _, key := range keys {
			drop = drop || mapping.Content[n].Value == key
		}
		if !drop {
			kept = append(kept, mapping.Content[n], mapping.Content[n+1])
		}
	}
	mapping.Content = kept
}
