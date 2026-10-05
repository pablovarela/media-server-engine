package secrets

import (
	"fmt"
	"strings"

	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	"github.com/getsops/sops/v3/cmd/sops/common"
	"github.com/getsops/sops/v3/config"
	"github.com/getsops/sops/v3/keyservice"
	"github.com/getsops/sops/v3/stores/dotenv"
	"github.com/getsops/sops/v3/version"
)

type Encrypter func(path string, plain []byte) ([]byte, error)

func SopsEncrypter(configBase, sopsConfig string) Encrypter {
	return func(path string, plain []byte) ([]byte, error) {
		if err := defaultAgeKeyFile(configBase); err != nil {
			return nil, err
		}
		rule, err := config.LoadCreationRuleForFile(sopsConfig, path, nil)
		if rule == nil && (err == nil || strings.Contains(err.Error(), "no matching creation rules")) {
			return nil, fmt.Errorf("%s has no creation rule for %s", sopsConfig, path)
		}
		if err != nil {
			return nil, err
		}
		store := &dotenv.Store{}
		branches, err := store.LoadPlainFile(plain)
		if err != nil {
			return nil, fmt.Errorf("%s isn't a dotenv file", path)
		}
		tree := sops.Tree{Branches: branches, Metadata: metadataFor(rule)}
		dataKey, errs := tree.GenerateDataKeyWithKeyServices([]keyservice.KeyServiceClient{keyservice.NewLocalClient()})
		if len(errs) > 0 {
			return nil, fmt.Errorf("could not make a data key for %s: %v", path, errs)
		}
		if err := common.EncryptTree(common.EncryptTreeOpts{DataKey: dataKey, Tree: &tree, Cipher: aes.NewCipher()}); err != nil {
			return nil, err
		}
		return store.EmitEncryptedFile(tree)
	}
}

func metadataFor(rule *config.Config) sops.Metadata {
	return sops.Metadata{
		KeyGroups:         rule.KeyGroups,
		ShamirThreshold:   rule.ShamirThreshold,
		UnencryptedSuffix: rule.UnencryptedSuffix,
		EncryptedSuffix:   rule.EncryptedSuffix,
		UnencryptedRegex:  rule.UnencryptedRegex,
		EncryptedRegex:    rule.EncryptedRegex,
		MACOnlyEncrypted:  rule.MACOnlyEncrypted,
		Version:           version.Version,
	}
}
