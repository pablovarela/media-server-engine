package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/apply"
	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/healthchecks"
	"github.com/pablovarela/media-server-engine/internal/images"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/paint"
	"github.com/pablovarela/media-server-engine/internal/process"
	"github.com/pablovarela/media-server-engine/internal/report"
	"github.com/pablovarela/media-server-engine/internal/secrets"
	"github.com/pablovarela/media-server-engine/internal/wiring"
)

var gluetunDependents = []string{"prowlarr", "flaresolverr", "deluge"}

func newApplyCommand(deps Dependencies) *cobra.Command {
	var afterUpdate string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply the config on disk to this machine: secrets, landing page, images and containers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("after-update") {
				return deps.apply(cmd)
			}
			if !afterUpdateRunID.MatchString("--after-update=" + afterUpdate) {
				return errors.New("--after-update takes the run id of the update that handed over, six hex digits")
			}
			i, err := deps.anyInstallation(cmd)
			if err != nil {
				return err
			}
			return deps.reported(cmd, i, func() error { return deps.apply(cmd) })
		},
	}
	// Older releases hand over with exactly `apply --after-update=<run id> --installation <name> [--verbose]`.
	command.Flags().StringVar(&afterUpdate, "after-update", "", "the run id of the update that handed over to this apply")
	_ = command.Flags().MarkHidden("after-update")
	return command
}

func (d Dependencies) reported(cmd *cobra.Command, i *installation.Installation, do func() error) (err error) {
	finishing := context.WithoutCancel(cmd.Context())
	defer func() {
		crashed := recover()
		if crashed != nil {
			err = fmt.Errorf("%v", crashed)
			defer panic(crashed)
		}
		if errors.Is(err, errHandedOver) {
			return
		}
		defer shieldSignals()()
		pings := d.pinger(cmd, i)
		if err != nil {
			pings.Ping(finishing, "update", "/fail")
			return
		}
		pings.Ping(finishing, "update", "")
	}()
	return do()
}

func (d Dependencies) apply(cmd *cobra.Command) error {
	o, err := d.openProject(cmd, compose.Stack, drawingWhenPinned)
	if err != nil {
		return err
	}
	wired, err := d.wiredProject(cmd, o)
	if err != nil {
		return err
	}
	wires, err := d.wiringFor(cmd, o.installation, wired)
	if err != nil {
		return err
	}
	return (&apply.Apply{
		Stack:    appliedStack{runner: o.runner, project: o.project, wired: wired, outcomes: o.outcomes, data: o.installation.Data},
		Checks:   d.checks(o.installation, o.network),
		Wiring:   wires,
		Page:     pageFunc(func(ctx context.Context) error { return d.applyPage(ctx, o) }),
		Images:   imagesFunc(func(ctx context.Context) error { return d.pruneImages(ctx, o.installation) }),
		MkdirAll: func(path string) error { return os.MkdirAll(path, 0o755) }, //nolint:gosec // containers running as other users read these folders
		Sleep:    d.Pause,
		Report:   report.From(cmd.Context()),
	}).Run(cmd.Context())
}

func (d Dependencies) wiringFor(cmd *cobra.Command, i *installation.Installation, wired *types.Project) (apply.Wiring, error) {
	if d.WiringSteps == nil {
		return nil, nil
	}
	r := report.From(cmd.Context())
	configarr := func(ctx context.Context, out io.Writer) (int, error) {
		runner, err := d.Compose(r.Tool("compose"), &compose.Outcomes{})
		if err != nil {
			return 0, err
		}
		return runner.RunOnce(ctx, wired, "configarr", out)
	}
	steps, err := d.WiringSteps(configarr, r.Tool("configarr"))
	if err != nil {
		return nil, err
	}
	appSecrets := secrets.Dotenv(readSecretsFile(i, "apps.env"))
	return &wiring.Wiring{
		Env: wiring.Env{
			Settings: i.Settings, Secrets: appSecrets, Config: i.Config, Data: i.Data,
			HTTP: &http.Client{Transport: d.HTTP.Transport}, Pause: d.Pause, Say: r.Say, Redact: wiring.NewRedactor(appSecrets),
		},
		Steps:  steps,
		Health: wiring.HealthChecks,
		Now:    d.Now,
		Warn:   func(line string) { r.Warn(paint.Stderr.Warning(line)) },
	}, nil
}

func readSecretsFile(i *installation.Installation, name string) []byte {
	content, _ := os.ReadFile(filepath.Join(i.State, ".secrets", name)) //nolint:gosec // the installation's decrypted secrets
	return content
}

func (d Dependencies) wiredProject(cmd *cobra.Command, o opened) (*types.Project, error) {
	profiles, err := compose.Profiles(o.installation, compose.Stack, true)
	if err != nil {
		return nil, err
	}
	return o.runner.Load(cmd.Context(), o.installation, compose.Stack, compose.Variables(o.installation, o.network, compose.DockerGID()), profiles)
}

func (d Dependencies) pruneImages(ctx context.Context, i *installation.Installation) error {
	docker, err := d.Images()
	if err != nil {
		return err
	}
	return images.Prune(ctx, docker, i, report.From(ctx))
}

func (d Dependencies) checks(i *installation.Installation, network string) apply.Checks {
	key := healthchecksKey(i, "HEALTHCHECKS_MANAGE_KEY")
	if key == "" || d.Systemd == nil || !d.Systemd() {
		return nil
	}
	return checksFunc(func(ctx context.Context) ([]string, []string) {
		facts := healthchecks.Facts{
			Name: i.Name, TimeZone: d.timeZone(ctx), Repository: d.repositoryLocation(i), SSH: d.Environment("USER") + "@" + network,
		}
		manage := healthchecks.Manage{Client: d.HTTP, URL: healthchecks.ChecksURL, Key: key}
		return manage.SetUp(ctx, healthchecks.ChecksFor(i.Name, i.Role(), d.shortHost()), facts)
	})
}

func (d Dependencies) timeZone(ctx context.Context) string {
	result, err := d.Run(io.Discard, io.Discard).Output(ctx, process.Command{Name: "timedatectl", Args: []string{"show", "-p", "Timezone", "--value"}})
	if zone := strings.TrimSpace(string(result.Stdout)); err == nil && result.Exit == 0 && zone != "" {
		return zone
	}
	link, err := os.Readlink(d.LocalTime)
	if _, zone, found := strings.Cut(link, "zoneinfo/"); err == nil && found {
		return zone
	}
	return "Etc/UTC"
}

func (d Dependencies) repositoryLocation(i *installation.Installation) string {
	if location := i.Settings["RESTIC_REPOSITORY"]; location != "" {
		return location
	}
	decrypted, err := d.Decrypt(filepath.Join(i.Config, "secrets", "backup.sops.env"))
	if err != nil {
		return ""
	}
	return secrets.Dotenv(decrypted)["RESTIC_REPOSITORY"]
}

type appliedStack struct {
	runner   composeRunner
	project  *types.Project
	wired    *types.Project
	outcomes *compose.Outcomes
	data     string
}

func (s appliedStack) BindSources() []string { return compose.BindSources(s.wired, s.data) }

func (s appliedStack) Pull(ctx context.Context) (string, error) {
	pulled, err := s.runner.Pull(ctx, s.wired)
	if _, err := s.outcome(err); err != nil {
		return "", err
	}
	return describePull(pulled), nil
}

func describePull(pulled compose.Pulled) string {
	upToDate := pulled.Total - pulled.New
	switch {
	case pulled.Total == 0:
		return "nothing to pull"
	case pulled.New == 0:
		return fmt.Sprintf("%d up to date", upToDate)
	}
	noun := "images"
	if pulled.New == 1 {
		noun = "image"
	}
	described := fmt.Sprintf("pulled %d new %s", pulled.New, noun)
	if upToDate > 0 {
		described += fmt.Sprintf(", %d up to date", upToDate)
	}
	return described
}

func (s appliedStack) Up(ctx context.Context) (string, error) {
	return s.outcome(s.runner.Up(ctx, s.project, nil, compose.NoWait))
}

func (s appliedStack) Reattach(ctx context.Context) (string, error) {
	detached, err := s.runner.Detached(ctx, s.project, "gluetun", gluetunDependents)
	if err != nil || len(detached) == 0 {
		return "nothing to reattach", err
	}
	if _, err := s.outcome(s.runner.Recreate(ctx, s.project, detached)); err != nil {
		return "", err
	}
	return "recreated " + strings.Join(detached, ", "), nil
}

func (s appliedStack) outcome(err error) (string, error) {
	outcome := s.outcomes.Take()
	if err != nil {
		return "", err
	}
	if len(outcome.Failed) > 0 {
		return "", errors.New(strings.Join(outcome.Failed, "; "))
	}
	if result := outcome.String(); result != "" {
		return result, nil
	}
	return "up to date", nil
}

type pageFunc func(context.Context) error

func (f pageFunc) Reload(ctx context.Context) error { return f(ctx) }

type imagesFunc func(context.Context) error

func (f imagesFunc) Prune(ctx context.Context) error { return f(ctx) }

type checksFunc func(context.Context) ([]string, []string)

func (f checksFunc) SetUp(ctx context.Context) ([]string, []string) { return f(ctx) }
