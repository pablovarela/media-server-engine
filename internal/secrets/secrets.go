package secrets

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pablovarela/media-server-engine/internal/installation"
)

type Decrypter func(path string) ([]byte, error)

var configarrSecret = regexp.MustCompile(`!secret\s+([A-Za-z0-9_]+)`)

func Dotenv(text []byte) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(string(text), "\n") {
		if key, value, found := strings.Cut(line, "="); found && key != "" {
			values[key] = value
		}
	}
	return values
}

func RandomKey() (string, error) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	return hex.EncodeToString(key), nil
}

func Apps(i *installation.Installation, decrypt Decrypter) (map[string]string, error) {
	text, err := decrypt(filepath.Join(i.Config, "secrets", "apps.sops.env"))
	if err != nil {
		return nil, fmt.Errorf("decrypt apps.sops.env: %w", err)
	}
	return Dotenv(text), nil
}

type sources struct {
	vpn          []byte
	apps         []byte
	healthchecks []byte
}

func WriteAll(i *installation.Installation, decrypt Decrypter, randomKey func() (string, error)) error {
	dir := filepath.Join(i.State, ".secrets")
	for _, d := range []string{dir, filepath.Join(dir, "configarr")} {
		if err := privateDirectory(d); err != nil {
			return err
		}
	}
	decrypted, err := decryptSources(i, decrypt)
	if err != nil {
		return err
	}
	key, err := gluetunControlKey(i, randomKey)
	if err != nil {
		return err
	}
	configarr, err := configarrSecrets(i, decrypted.apps)
	if err != nil {
		return err
	}
	for name, content := range secretFiles(decrypted, key, configarr) {
		if err := writePrivate(filepath.Join(dir, name), content); err != nil {
			return err
		}
	}
	return createIfMissing(filepath.Join(dir, "homepage.env"))
}

func decryptSources(i *installation.Installation, decrypt Decrypter) (sources, error) {
	decrypted := func(name string) ([]byte, error) {
		text, err := decrypt(filepath.Join(i.Config, "secrets", name))
		if err != nil {
			return nil, fmt.Errorf("decrypt %s: %w", name, err)
		}
		return text, nil
	}
	var found sources
	var err error
	if found.vpn, err = decrypted("vpn.sops.env"); err != nil {
		return sources{}, err
	}
	if found.apps, err = decrypted("apps.sops.env"); err != nil {
		return sources{}, err
	}
	if _, statErr := os.Stat(filepath.Join(i.Config, "secrets", "healthchecks.sops.env")); statErr == nil {
		if found.healthchecks, err = decrypted("healthchecks.sops.env"); err != nil {
			return sources{}, err
		}
	}
	return found, nil
}

func secretFiles(decrypted sources, gluetunKey string, configarr []byte) map[string][]byte {
	keys := Dotenv(decrypted.apps)
	files := map[string][]byte{
		"vpn.env":               decrypted.vpn,
		"apps.env":              decrypted.apps,
		"healthchecks.env":      decrypted.healthchecks,
		"gluetun.env":           []byte(`HTTP_CONTROL_SERVER_AUTH_DEFAULT_ROLE={"auth":"apikey","apikey":"` + gluetunKey + `"}` + "\n"),
		"configarr/secrets.yml": configarr,
		"portainer_admin":       []byte(keys["PORTAINER_ADMIN_PASSWORD"]),
	}
	for _, app := range []string{"SONARR", "RADARR", "PROWLARR"} {
		files[strings.ToLower(app)+".env"] = []byte(app + "__AUTH__APIKEY=" + keys[app+"_API_KEY"] + "\n")
	}
	return files
}

func createIfMissing(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return file.Close()
}

func gluetunControlKey(i *installation.Installation, randomKey func() (string, error)) (string, error) {
	path := filepath.Join(i.Data, "volumes", ".wiring", "gluetun-control.key")
	if text, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(text)) != "" {
		return strings.TrimSpace(string(text)), nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := privateDirectory(filepath.Dir(path)); err != nil {
		return "", err
	}
	key, err := randomKey()
	if err != nil {
		return "", err
	}
	return key, writePrivate(path, []byte(key+"\n"))
}

func configarrSecrets(i *installation.Installation, apps []byte) ([]byte, error) {
	config, err := os.ReadFile(filepath.Join(i.Config, "configarr", "config.yml"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, match := range configarrSecret.FindAllStringSubmatch(string(config), -1) {
		wanted[match[1]] = true
	}
	values := Dotenv(apps)
	var names []string
	for name := range values {
		if wanted[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var out bytes.Buffer
	for _, name := range names {
		quoted, err := json.Marshal(values[name])
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&out, "%s: %s\n", name, quoted)
	}
	return out.Bytes(), nil
}

func privateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700) //nolint:gosec // a directory needs its execute bit
}

func writePrivate(path string, content []byte) error {
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, content) {
		return os.Chmod(path, 0o600)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
