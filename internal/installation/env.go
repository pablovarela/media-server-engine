package installation

import (
	"os"
	"regexp"
	"strings"
)

func ParseEnv(text string, lookup func(string) string) map[string]string {
	settings := map[string]string{}
	variable := func(name string) string {
		if value, found := settings[name]; found {
			return value
		}
		return lookup(name)
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		if !found {
			continue
		}
		settings[strings.TrimSpace(key)] = expanded(value, variable)
	}
	return settings
}

var trailingComment = regexp.MustCompile(`\s+#.*$`)

func expanded(value string, variable func(string) string) string {
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1]
	}
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		return os.Expand(value[1:len(value)-1], variable)
	}
	value = trailingComment.ReplaceAllString(value, "")
	if value == "~" || strings.HasPrefix(value, "~/") {
		value = variable("HOME") + value[1:]
	}
	return os.Expand(value, variable)
}
