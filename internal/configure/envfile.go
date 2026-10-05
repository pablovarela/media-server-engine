package configure

import (
	"regexp"
	"strings"
)

type Update struct{ Key, Value string }

var trailingComment = regexp.MustCompile(`\s+#.*$`)

func ReadEnv(text string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if key, value, found := assignment(line); found {
			values[key] = unquoted(value)
		}
	}
	return values
}

func RewriteEnv(text string, updates []Update, quote bool) string {
	pending := map[string]string{}
	for _, u := range updates {
		pending[u.Key] = u.Value
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if text == "" {
		lines = nil
	}
	present := map[string]bool{}
	for i, line := range lines {
		key, old, found := assignment(line)
		value, updated := pending[key]
		if !found || !updated {
			continue
		}
		lines[i] = exportPrefix(line) + key + "=" + written(value, quote) + inlineComment(old)
		present[key] = true
	}
	for _, u := range updates {
		if !present[u.Key] {
			lines = append(lines, u.Key+"="+written(u.Value, quote))
			present[u.Key] = true
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func inlineComment(value string) string {
	if strings.HasPrefix(value, "'") || strings.HasPrefix(value, `"`) {
		return ""
	}
	return trailingComment.FindString(value)
}

func assignment(line string) (key, value string, found bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	key, value, found = strings.Cut(strings.TrimPrefix(trimmed, "export "), "=")
	return strings.TrimSpace(key), value, found && strings.TrimSpace(key) != ""
}

func exportPrefix(line string) string {
	indented := strings.TrimLeft(line, " \t")
	prefix := line[:len(line)-len(indented)]
	if strings.HasPrefix(indented, "export ") {
		prefix += "export "
	}
	return prefix
}

func unquoted(value string) string {
	for _, quote := range []byte{'\'', '"'} {
		if len(value) >= 2 && value[0] == quote && value[len(value)-1] == quote {
			return value[1 : len(value)-1]
		}
	}
	return trailingComment.ReplaceAllString(value, "")
}

func written(value string, quote bool) string {
	switch {
	case !quote || !strings.ContainsAny(value, " \t#\"'"):
		return value
	case strings.Contains(value, `"`):
		return "'" + value + "'"
	}
	return `"` + value + `"`
}
