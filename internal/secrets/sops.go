package secrets

import "github.com/getsops/sops/v3/decrypt"

func Sops(path string) ([]byte, error) {
	return decrypt.File(path, "dotenv")
}
