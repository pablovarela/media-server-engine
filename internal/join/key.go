package join

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"filippo.io/age"

	"github.com/pablovarela/media-server-engine/internal/create"
)

var (
	errNotAKey      = errors.New("that isn't an age secret key; it starts with AGE-SECRET-KEY-1")
	errNotTheirKey  = errors.New("that key isn't one of this installation's keys")
	errNoRecipients = errors.New("the config has no encrypted secrets to check the key against")
)

const recipientKey = "__map_recipient="

func MatchKey(pasted, config string) (create.Key, error) {
	identity, err := age.ParseX25519Identity(secretLine(pasted))
	if err != nil {
		return create.Key{}, errNotAKey
	}
	public := identity.Recipient().String()
	byFile, err := recipientsByFile(config)
	if err != nil {
		return create.Key{}, err
	}
	var missing []string
	for file, recipients := range byFile {
		if !slices.Contains(recipients, public) {
			missing = append(missing, file)
		}
	}
	switch {
	case len(missing) == len(byFile):
		return create.Key{}, errNotTheirKey
	case len(missing) > 0:
		slices.Sort(missing)
		return create.Key{}, fmt.Errorf("that key opens only some of this installation's secret files (not %s)", strings.Join(missing, ", "))
	}
	return create.Key{Public: public, Secret: identity.String()}, nil
}

func secretLine(pasted string) string {
	for _, word := range strings.Fields(pasted) {
		if strings.HasPrefix(word, "AGE-SECRET-KEY-") {
			return word
		}
	}
	return strings.TrimSpace(pasted)
}

func Recipients(config string) ([]string, error) {
	byFile, err := recipientsByFile(config)
	if err != nil {
		return nil, err
	}
	var recipients []string
	for _, found := range byFile {
		for _, r := range found {
			if !slices.Contains(recipients, r) {
				recipients = append(recipients, r)
			}
		}
	}
	slices.Sort(recipients)
	return recipients, nil
}

func recipientsByFile(config string) (map[string][]string, error) {
	files, err := filepath.Glob(filepath.Join(config, "secrets", "*.sops.env"))
	if err != nil {
		return nil, err
	}
	byFile := map[string][]string{}
	for _, file := range files {
		found, err := recipientsIn(file)
		if err != nil {
			return nil, err
		}
		if len(found) > 0 {
			byFile[filepath.Join("secrets", filepath.Base(file))] = found
		}
	}
	if len(byFile) == 0 {
		return nil, errNoRecipients
	}
	return byFile, nil
}

func recipientsIn(file string) ([]string, error) {
	f, err := os.Open(file) //nolint:gosec // a secrets file of the cloned config
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var found []string
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		line := lines.Text()
		if strings.HasPrefix(line, "sops_age__list_") {
			if _, recipient, ok := strings.Cut(line, recipientKey); ok {
				found = append(found, recipient)
			}
		}
	}
	return found, lines.Err()
}
