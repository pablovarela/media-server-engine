package installation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

var ErrNewerSchema = errors.New("the config needs a newer mse")

type newerSchema struct{ message string }

func (e newerSchema) Error() string { return e.message }

func (e newerSchema) Is(target error) bool { return target == ErrNewerSchema }

func (i *Installation) CheckSchema(engineMajor int) error {
	path := filepath.Join(i.Config, "config.yml")
	text, err := os.ReadFile(path) //nolint:gosec // reads the installation's own config.yml
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s is missing: add it with config: %d", path, engineMajor)
	}
	if err != nil {
		return err
	}
	var declared struct {
		Config *int `yaml:"config"`
	}
	if err := yaml.Unmarshal(text, &declared); err != nil {
		return fmt.Errorf("%s: config must be a whole number", path)
	}
	if declared.Config == nil {
		return fmt.Errorf("%s has no config: version", path)
	}
	switch schema := *declared.Config; {
	case schema > engineMajor:
		return newerSchema{fmt.Sprintf("this config is schema %d and this mse reads %d: update mse", schema, engineMajor)}
	case schema < engineMajor:
		return fmt.Errorf("this config is schema %d and this mse reads %d: migrate the config", schema, engineMajor)
	}
	return nil
}
