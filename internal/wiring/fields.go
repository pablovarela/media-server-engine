package wiring

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	nameKey       = "name"
	apiKeyHeader  = "X-Api-Key"      //nolint:gosec // a header name, not a credential
	sonarrKeyName = "SONARR_API_KEY" //nolint:gosec // the name of a secret, not its value
	radarrKeyName = "RADARR_API_KEY" //nolint:gosec // the name of a secret, not its value
	apiKeyField   = "apiKey"
	portField     = "port"
	hostnameField = "hostname"
	useSSLField   = "useSsl"
	baseURLField  = "baseUrl"
	pythonFalse   = "False"
)

func field(item map[string]any, name string) any {
	for _, entry := range list(item["fields"]) {
		if f, ok := entry.(map[string]any); ok && f[nameKey] == name {
			return f["value"]
		}
	}
	return nil
}

func setField(item map[string]any, name string, value any) {
	fields := list(item["fields"])
	for _, entry := range fields {
		if f, ok := entry.(map[string]any); ok && f[nameKey] == name {
			f["value"] = value
			return
		}
	}
	item["fields"] = append(fields, map[string]any{nameKey: name, "value": value})
}

func byName(items []map[string]any, name string) map[string]any {
	for _, item := range items {
		if item[nameKey] == name {
			return item
		}
	}
	return nil
}

func list(value any) []any {
	items, _ := value.([]any)
	return items
}

func entries(value any) []map[string]any {
	var found []map[string]any
	for _, item := range list(value) {
		if entry, ok := item.(map[string]any); ok {
			found = append(found, entry)
		}
	}
	return found
}

func section(value any) map[string]any {
	if found, ok := value.(map[string]any); ok {
		return found
	}
	return map[string]any{}
}

func text(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return show(value)
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func same(a, b any) bool {
	left, errLeft := json.Marshal(numeric(a))
	right, errRight := json.Marshal(numeric(b))
	return errLeft == nil && errRight == nil && string(left) == string(right)
}

func numeric(value any) any {
	switch v := value.(type) {
	case pythonNumber:
		parsed, err := strconv.ParseFloat(string(v), 64)
		if err != nil {
			return string(v)
		}
		return parsed
	case pythonFloat:
		return float64(v)
	case int:
		return float64(v)
	case []any:
		converted := make([]any, len(v))
		for n, item := range v {
			converted[n] = numeric(item)
		}
		return converted
	case map[string]any:
		converted := make(map[string]any, len(v))
		for key, item := range v {
			converted[key] = numeric(item)
		}
		return converted
	}
	return value
}

func contains(items []any, value any) bool {
	for _, item := range items {
		if same(item, value) {
			return true
		}
	}
	return false
}

func show(value any) string {
	switch v := value.(type) {
	case nil:
		return "None"
	case bool:
		if v {
			return "True"
		}
		return pythonFalse
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case []any:
		parts := make([]string, len(v))
		for n, item := range v {
			parts[n] = show(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return fmt.Sprint(value)
}
