package join

import (
	"bufio"
	"errors"
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
	recipients, err := Recipients(config)
	if err != nil {
		return create.Key{}, err
	}
	public := identity.Recipient().String()
	if !slices.Contains(recipients, public) {
		return create.Key{}, errNotTheirKey
	}
	return create.Key{Public: public, Secret: identity.String()}, nil
}

func secretLine(pasted string) string {
	for _, line := range strings.Split(pasted, "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "AGE-SECRET-KEY-") {
			return line
		}
	}
	return strings.TrimSpace(pasted)
}

func Recipients(config string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(config, "secrets", "*.sops.env"))
	if err != nil {
		return nil, err
	}
	var recipients []string
	for _, file := range files {
		found, err := recipientsIn(file)
		if err != nil {
			return nil, err
		}
		for _, r := range found {
			if !slices.Contains(recipients, r) {
				recipients = append(recipients, r)
			}
		}
	}
	if len(recipients) == 0 {
		return nil, errNoRecipients
	}
	return recipients, nil
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
