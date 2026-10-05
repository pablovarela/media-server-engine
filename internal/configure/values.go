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

type Texts struct {
	Plain map[string][]byte
	Raw   map[string][]byte
}

func Load(config string, decrypt secrets.Decrypter) (Values, Texts, error) {
	values := Values{}
	texts := Texts{Plain: map[string][]byte{}, Raw: map[string][]byte{}}
	for _, file := range append([]string{PlainFile}, secretFiles()...) {
		raw, err := os.ReadFile(filepath.Join(config, file)) //nolint:gosec // a file of the installation's own config
		if errors.Is(err, fs.ErrNotExist) {
			values[file] = map[string]string{}
			continue
		}
		if err != nil {
			return nil, Texts{}, err
		}
		plain := raw
		if file != PlainFile {
			if plain, err = decrypt(filepath.Join(config, file)); err != nil {
				return nil, Texts{}, fmt.Errorf("could not read %s: %w", file, err)
			}
		}
		values[file] = parsed(file, plain)
		texts.Raw[file], texts.Plain[file] = raw, plain
	}
	return values, texts, nil
}

func parsed(file string, plain []byte) map[string]string {
	if file == PlainFile {
		return ReadEnv(string(plain))
	}
	return secrets.Dotenv(plain)
}
