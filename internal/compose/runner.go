package compose

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/cmd/display"
	"github.com/docker/compose/v5/pkg/api"
	sdk "github.com/docker/compose/v5/pkg/compose"

	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/paint"
)

type service interface {
	LoadProject(ctx context.Context, options api.ProjectLoadOptions) (*types.Project, error)
	Up(ctx context.Context, project *types.Project, options api.UpOptions) error
	Down(ctx context.Context, projectName string, options api.DownOptions) error
	Ps(ctx context.Context, projectName string, options api.PsOptions) ([]api.ContainerSummary, error)
	Logs(ctx context.Context, projectName string, consumer api.LogConsumer, options api.LogOptions) error
	Restart(ctx context.Context, projectName string, options api.RestartOptions) error
}

type Container struct {
	Name   string
	State  string
	Health string
	Ports  []string
}

type LogsOptions struct {
	Follow   bool
	Tail     string
	Services []string
}

type Runner struct {
	service service
}

func NewRunner(out, errOut io.Writer) (*Runner, error) {
	dockerCLI, err := command.NewDockerCli(command.WithOutputStream(out), command.WithErrorStream(errOut))
	if err != nil {
		return nil, err
	}
	if err := dockerCLI.Initialize(&flags.ClientOptions{}); err != nil {
		return nil, err
	}
	composeService, err := sdk.NewComposeService(dockerCLI, sdk.WithEventProcessor(display.Plain(errOut)))
	if err != nil {
		return nil, err
	}
	return &Runner{service: composeService}, nil
}

func (r *Runner) Load(ctx context.Context, i *installation.Installation, kind Kind, variables, profiles []string) (*types.Project, error) {
	paths := []string{filepath.Join(i.State, kind.EngineFile), filepath.Join(i.Config, kind.ImagesFile)}
	if kind.Override {
		if override := filepath.Join(i.Config, "compose.override.yml"); exists(override) {
			paths = append(paths, override)
		}
	}
	return r.service.LoadProject(ctx, api.ProjectLoadOptions{
		ProjectName:       kind.Name,
		ConfigPaths:       paths,
		WorkingDir:        i.State,
		Profiles:          profiles,
		Offline:           true,
		ProjectOptionsFns: []cli.ProjectOptionsFn{cli.WithEnv(variables)},
	})
}

type Wait struct {
	Enabled bool
	Timeout time.Duration
}

var NoWait = Wait{}

func (r *Runner) Up(ctx context.Context, project *types.Project, services []string, wait Wait) error {
	if len(services) > 0 {
		selected, err := project.WithSelectedServices(services)
		if err != nil {
			return err
		}
		project = selected
	}
	return r.service.Up(ctx, project, api.UpOptions{
		Create: api.CreateOptions{Services: services, RemoveOrphans: true, Inherit: true},
		Start:  api.StartOptions{Project: project, Services: services, Wait: wait.Enabled, WaitTimeout: wait.Timeout},
	})
}

func (r *Runner) Down(ctx context.Context, project *types.Project) error {
	return r.service.Down(ctx, project.Name, api.DownOptions{Project: project, RemoveOrphans: true})
}

func (r *Runner) Restart(ctx context.Context, project *types.Project, services []string) error {
	return r.service.Restart(ctx, project.Name, api.RestartOptions{Project: project, Services: services})
}

func (r *Runner) Ps(ctx context.Context, project *types.Project) ([]Container, error) {
	summaries, err := r.service.Ps(ctx, project.Name, api.PsOptions{Project: project, All: true})
	if err != nil {
		return nil, err
	}
	containers := make([]Container, 0, len(summaries))
	for _, summary := range summaries {
		containers = append(containers, Container{
			Name:   summary.Name,
			State:  string(summary.State),
			Health: string(summary.Health),
			Ports:  publishedPorts(summary.Publishers),
		})
	}
	sort.Slice(containers, func(a, b int) bool { return containers[a].Name < containers[b].Name })
	return containers, nil
}

func (r *Runner) Logs(ctx context.Context, project *types.Project, options LogsOptions, w io.Writer) error {
	return r.service.Logs(ctx, project.Name, &lines{w: w}, api.LogOptions{
		Project: project, Follow: options.Follow, Tail: options.Tail, Services: options.Services,
	})
}

type lines struct {
	mu       sync.Mutex
	w        io.Writer
	services paint.Services
}

func (l *lines) Log(container, message string) { l.write(container, message) }

func (l *lines) Err(container, message string) { l.write(container, message) }

func (l *lines) Status(string, string) {}

func (l *lines) write(container, message string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = fmt.Fprintf(l.w, "%s | %s\n", l.services.Paint(container), message)
}

func publishedPorts(publishers api.PortPublishers) []string {
	var ports []string
	seen := map[string]bool{}
	for _, publisher := range publishers {
		port := fmt.Sprintf("%d->%d/%s", publisher.PublishedPort, publisher.TargetPort, publisher.Protocol)
		if publisher.PublishedPort != 0 && !seen[port] {
			seen[port] = true
			ports = append(ports, port)
		}
	}
	sort.Slice(ports, func(a, b int) bool { return portBefore(ports[a], ports[b]) })
	return ports
}

func portBefore(a, b string) bool {
	if portNumber(a) != portNumber(b) {
		return portNumber(a) < portNumber(b)
	}
	return a < b
}

func portNumber(port string) int {
	published, _, _ := strings.Cut(port, "->")
	number, _ := strconv.Atoi(published)
	return number
}
