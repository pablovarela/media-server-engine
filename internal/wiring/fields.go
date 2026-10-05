package wiring

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const nameKey = "name"

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
	left, errLeft := json.Marshal(a)
	right, errRight := json.Marshal(b)
	return errLeft == nil && errRight == nil && string(left) == string(right)
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
		return "False"
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
