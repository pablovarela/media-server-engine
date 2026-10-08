package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/configure"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/report"
	"github.com/pablovarela/media-server-engine/internal/secrets"
)

func newCheckConfigCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "check-config [dir]",
		Short: "Check a config folder without its secrets key: its schema, settings, sealed secrets and stack (for the config's CI)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			config, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			r := report.From(cmd.Context())
			problems := 0
			for _, check := range deps.configChecks(cmd, config) {
				step := r.Step(check.title)
				result, err := check.run()
				if err != nil {
					problems++
					_ = step.FailWithoutTail(err)
					for _, line := range strings.Split(err.Error(), "\n") {
						r.Warn("  " + line)
					}
					continue
				}
				step.Done(result)
			}
			switch {
			case problems == 1:
				return fmt.Errorf("%s has 1 problem", dir)
			case problems > 1:
				return fmt.Errorf("%s has %d problems", dir, problems)
			}
			return nil
		},
	}
}

type configCheck struct {
	title string
	run   func() (string, error)
}

func (d Dependencies) configChecks(cmd *cobra.Command, config string) []configCheck {
	return []configCheck{
		{"Checking the config's schema", func() (string, error) {
			i := &installation.Installation{Config: config}
			major := d.Build.Major()
			return fmt.Sprintf("config %d", major), i.CheckSchema(major)
		}},
		{"Checking the settings", func() (string, error) {
			values, _, err := configure.Load(config, sealedValues(config))
			if err != nil {
				return "", err
			}
			return "done", errors.Join(configure.Problems(values)...)
		}},
		{"Checking the secrets", func() (string, error) {
			files, err := filepath.Glob(filepath.Join(config, "secrets", "*.sops.env"))
			if err != nil {
				return "", err
			}
			for _, file := range files {
				if _, err := secrets.SealedKeys(filepath.Join(config, ".sops.yaml"), file); err != nil {
					return "", err
				}
			}
			return fmt.Sprintf("%d files", len(files)), nil
		}},
		{"Loading the stack", func() (string, error) {
			values, _, _ := configure.Load(config, sealedValues(config))
			return "done", compose.LoadOffline(cmd.Context(), d.Engine, config, values[configure.PlainFile])
		}},
	}
}

func sealedValues(config string) secrets.Decrypter {
	return func(path string) ([]byte, error) {
		keys, err := secrets.SealedKeys(filepath.Join(config, ".sops.yaml"), path)
		if err != nil {
			return nil, err
		}
		var lines strings.Builder
		for _, key := range keys {
			lines.WriteString(key + "=sealed-by-sops\n")
		}
		return []byte(lines.String()), nil
	}
}
