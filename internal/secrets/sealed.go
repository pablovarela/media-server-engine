package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/config"
	"github.com/getsops/sops/v3/stores/dotenv"
)

func SealedKeys(sopsConfig, path string) (map[string]bool, error) {
	shown := filepath.Join(filepath.Base(filepath.Dir(path)), filepath.Base(path))
	text, err := os.ReadFile(path) //nolint:gosec // a secrets file of the config being checked
	if err != nil {
		return nil, err
	}
	tree, err := (&dotenv.Store{}).LoadEncryptedFile(text)
	if err != nil {
		return nil, fmt.Errorf("%s isn't encrypted by sops", shown)
	}
	rule, err := config.LoadCreationRuleForFile(sopsConfig, path, nil)
	if err != nil || rule == nil {
		return nil, fmt.Errorf(".sops.yaml has no rule for %s", shown)
	}
	have, want := recipients(tree.Metadata.KeyGroups), recipients(rule.KeyGroups)
	if !slices.Equal(have, want) {
		return nil, fmt.Errorf("%s is encrypted for %s, not for %s as .sops.yaml says", shown, strings.Join(have, ", "), strings.Join(want, ", "))
	}
	held := map[string]bool{}
	for _, item := range tree.Branches[0] {
		name, ok := item.Key.(string)
		if !ok {
			continue
		}
		value, _ := item.Value.(string)
		if value != "" && !strings.HasPrefix(value, "ENC[") {
			return nil, fmt.Errorf("%s: %s isn't encrypted; set it with mse configure", shown, name)
		}
		held[name] = value != ""
	}
	return held, nil
}

func recipients(groups []sops.KeyGroup) []string {
	var all []string
	for _, group := range groups {
		for _, key := range group {
			all = append(all, key.ToString())
		}
	}
	sort.Strings(all)
	return all
}
