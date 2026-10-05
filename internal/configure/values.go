package configure

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pablovarela/media-server-engine/internal/secrets"
)

type Values map[string]map[string]string

func (v Values) Get(file, key string) string {
	return v[file][key]
}

func (v Values) With(file, key, value string) Values {
	copied := Values{}
	for f, keys := range v {
		copied[f] = map[string]string{}
		for k, val := range keys {
			copied[f][k] = val
		}
	}
	if copied[file] == nil {
		copied[file] = map[string]string{}
	}
	copied[file][key] = value
	return copied
}

func Load(config string, decrypt secrets.Decrypter) (Values, map[string][]byte, error) {
	plain, err := readIfPresent(filepath.Join(config, PlainFile))
	if err != nil {
		return nil, nil, err
	}
	values := Values{PlainFile: ReadEnv(string(plain))}
	texts := map[string][]byte{PlainFile: plain}
	for _, file := range secretFiles() {
		path := filepath.Join(config, file)
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			values[file] = map[string]string{}
			continue
		}
		text, err := decrypt(path)
		if err != nil {
			return nil, nil, fmt.Errorf("could not read %s: %w", file, err)
		}
		values[file] = secrets.Dotenv(text)
		texts[file] = text
	}
	return values, texts, nil
}

func readIfPresent(path string) ([]byte, error) {
	text, err := os.ReadFile(path) //nolint:gosec // a file of the installation's own config
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return text, err
}
