package secrets

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/getsops/sops/v3/decrypt"
)

func Sops(configBase string) Decrypter {
	return func(path string) ([]byte, error) {
		if err := defaultAgeKeyFile(configBase); err != nil {
			return nil, err
		}
		text, err := decrypt.File(path, "dotenv")
		var explained interface{ UserError() string }
		if errors.As(err, &explained) {
			return nil, errors.New(explained.UserError())
		}
		return text, err
	}
}

func defaultAgeKeyFile(configBase string) error {
	for _, variable := range []string{"SOPS_AGE_KEY", "SOPS_AGE_KEY_FILE", "SOPS_AGE_KEY_CMD"} {
		if os.Getenv(variable) != "" {
			return nil
		}
	}
	keys := filepath.Join(configBase, "sops", "age", "keys.txt")
	if _, err := os.Stat(keys); err != nil {
		return nil
	}
	return os.Setenv("SOPS_AGE_KEY_FILE", keys)
}
