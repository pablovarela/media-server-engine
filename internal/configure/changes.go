package configure

import (
	"fmt"
	"slices"
	"strings"
)

const rotateSection = "Rotate keys"

type Change struct {
	Section, File, Key, Before, After string
}

func Diff(before, after Values) []Change {
	var changes []Change
	for _, section := range Sections() {
		for _, f := range section.Fields {
			if old, updated := before.Get(f.File, f.Key), after.Get(f.File, f.Key); old != updated {
				changes = append(changes, Change{Section: section.Name, File: f.File, Key: f.Key, Before: old, After: updated})
			}
		}
	}
	for _, app := range Rotatable {
		if old, updated := before.Get(AppsFile, app.Key), after.Get(AppsFile, app.Key); old != updated {
			changes = append(changes, Change{Section: rotateSection, File: AppsFile, Key: app.Key, Before: old, After: updated})
		}
	}
	return changes
}

func Summary(changes []Change) []string {
	lines := make([]string, 0, len(changes))
	for _, c := range changes {
		lines = append(lines, fmt.Sprintf("%-14s%s", c.Section, described(c)))
	}
	return lines
}

func described(c Change) string {
	switch {
	case c.File == PlainFile:
		return fmt.Sprintf("%s: %s -> %s", c.Key, orNone(c.Before), orNone(c.After))
	case c.Section == rotateSection || c.Before == "":
		return c.Key + " new"
	}
	return c.Key + " changed"
}

func orNone(value string) string {
	if value == "" {
		return "(none)"
	}
	return value
}

func CommitMessage(name string, changes []Change) string {
	var parts []string
	for _, c := range changes {
		part := c.Section
		if c.Section == rotateSection {
			part = "rotate " + appNamed(c.Key) + " key"
		}
		if !slices.Contains(parts, part) {
			parts = append(parts, part)
		}
	}
	return fmt.Sprintf("Configure %s: %s", name, strings.Join(parts, ", "))
}

func appNamed(key string) string {
	for _, app := range Rotatable {
		if app.Key == key {
			return app.Name
		}
	}
	return key
}

func Missing(v Values) []Section {
	var missing []Section
	for _, section := range Sections() {
		for _, f := range section.Fields {
			if v.Get(f.File, f.Key) == "" && f.ValidateIn(v) != nil {
				missing = append(missing, section)
				break
			}
		}
	}
	return missing
}

func Rotate(v Values, apps []App, randomKey func() (string, error)) (Values, error) {
	for _, app := range apps {
		key, err := randomKey()
		if err != nil {
			return nil, err
		}
		v = v.With(AppsFile, app.Key, key)
	}
	return v, nil
}
