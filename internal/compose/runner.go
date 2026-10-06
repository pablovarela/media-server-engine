package compose

import (
	"context"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/pkg/api"
	sdk "github.com/docker/compose/v5/pkg/compose"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

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
	Stop(ctx context.Context, projectName string, options api.StopOptions) error
	Start(ctx context.Context, projectName string, options api.StartOptions) error
	Pull(ctx context.Context, project *types.Project, options api.PullOptions) error
	Create(ctx context.Context, project *types.Project, options api.CreateOptions) error
}

type containers interface {
	ImageInspect(ctx context.Context, image string, options ...client.ImageInspectOption) (client.ImageInspectResult, error)
	ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerStart(ctx context.Context, containerID string, options client.ContainerStartOptions) (client.ContainerStartResult, error)
	ContainerLogs(ctx context.Context, containerID string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error)
	ContainerWait(ctx context.Context, containerID string, options client.ContainerWaitOptions) client.ContainerWaitResult
	ContainerStop(ctx context.Context, containerID string, options client.ContainerStopOptions) (client.ContainerStopResult, error)
	ContainerRemove(ctx context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error)
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
	docker  containers
}

func (k Kind) Title() string {
	if k.Name == Monitoring.Name {
		return "the monitoring stack"
	}
	return "the stack"
}

func NewRunner(tool io.Writer, outcomes *Outcomes) (*Runner, error) {
	dockerCLI, err := command.NewDockerCli(command.WithOutputStream(tool), command.WithErrorStream(tool))
	if err != nil {
		return nil, err
	}
	if err := dockerCLI.Initialize(&flags.ClientOptions{}); err != nil {
		return nil, err
	}
	composeService, err := sdk.NewComposeService(dockerCLI, sdk.WithEventProcessor(&events{tool: tool, outcomes: outcomes}))
	if err != nil {
		return nil, err
	}
	return &Runner{service: composeService, docker: dockerCLI.Client()}, nil
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

func (r *Runner) RunningServices(ctx context.Context, project *types.Project) ([]string, error) {
	summaries, err := r.service.Ps(ctx, project.Name, api.PsOptions{Project: project})
	if err != nil {
		return nil, err
	}
	inProject := project.ServiceNames()
	running := map[string]bool{}
	for _, summary := range summaries {
		if summary.State == container.StateRunning && slices.Contains(inProject, summary.Service) {
			running[summary.Service] = true
		}
	}
	services := slices.Collect(maps.Keys(running))
	slices.Sort(services)
	return services, nil
}

func (r *Runner) AnyRunning(ctx context.Context, project *types.Project) (bool, error) {
	summaries, err := r.service.Ps(ctx, project.Name, api.PsOptions{Project: project})
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(summaries, func(s api.ContainerSummary) bool { return s.State == container.StateRunning }), nil
}

func (r *Runner) Stop(ctx context.Context, project *types.Project) error {
	return r.service.Stop(ctx, project.Name, api.StopOptions{Project: project})
}

func (r *Runner) Start(ctx context.Context, project *types.Project, services []string) error {
	selected, err := project.WithSelectedServices(services)
	if err != nil {
		return err
	}
	return r.service.Start(ctx, project.Name, api.StartOptions{Project: selected})
}

type Pulled struct {
	New   int
	Total int
}

func (r *Runner) Pull(ctx context.Context, project *types.Project) (Pulled, error) {
	images := map[string]bool{}
	for _, service := range project.Services {
		if service.Image != "" {
			images[service.Image] = true
		}
	}
	before, err := r.imageIDs(ctx, images)
	if err != nil {
		return Pulled{}, err
	}
	if err := r.service.Pull(ctx, project, api.PullOptions{}); err != nil {
		return Pulled{}, err
	}
	after, err := r.imageIDs(ctx, images)
	if err != nil {
		return Pulled{}, err
	}
	pulled := Pulled{Total: len(images)}
	for image := range images {
		if before[image] != after[image] {
			pulled.New++
		}
	}
	return pulled, nil
}

func (r *Runner) imageIDs(ctx context.Context, images map[string]bool) (map[string]string, error) {
	ids := map[string]string{}
	for image := range images {
		inspected, err := r.docker.ImageInspect(ctx, image)
		switch {
		case cerrdefs.IsNotFound(err):
			ids[image] = ""
		case err != nil:
			return nil, err
		default:
			ids[image] = inspected.ID
		}
	}
	return ids, nil
}

func (r *Runner) Recreate(ctx context.Context, project *types.Project, services []string) error {
	selected, err := project.WithSelectedServices(services, types.IgnoreDependencies)
	if err != nil {
		return err
	}
	return r.service.Up(ctx, selected, api.UpOptions{
		Create: api.CreateOptions{Services: services, Recreate: api.RecreateForce, RecreateDependencies: api.RecreateNever, Inherit: true},
		Start:  api.StartOptions{Project: selected, Services: services},
	})
}

func (r *Runner) Detached(ctx context.Context, project *types.Project, from string, dependents []string) ([]string, error) {
	summaries, err := r.service.Ps(ctx, project.Name, api.PsOptions{Project: project, All: true})
	if err != nil {
		return nil, err
	}
	ids := map[string]string{}
	for _, summary := range summaries {
		if summary.Service != from || summary.State == container.StateRunning {
			ids[summary.Service] = summary.ID
		}
	}
	hub, running := ids[from]
	if !running {
		return nil, nil
	}
	var detached []string
	for _, dependent := range dependents {
		if _, inProject := project.Services[dependent]; !inProject {
			continue
		}
		attached, err := r.attachedTo(ctx, ids[dependent], hub)
		if err != nil {
			return nil, err
		}
		if !attached {
			detached = append(detached, dependent)
		}
	}
	return detached, nil
}

func (r *Runner) attachedTo(ctx context.Context, id, hub string) (bool, error) {
	if id == "" {
		return false, nil
	}
	inspected, err := r.docker.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return false, err
	}
	return inspected.Container.HostConfig != nil && string(inspected.Container.HostConfig.NetworkMode) == "container:"+hub, nil
}

func BindSources(project *types.Project, under string) []string {
	found := map[string]bool{}
	prefix := strings.TrimRight(under, "/") + "/"
	for _, service := range project.Services {
		for _, volume := range service.Volumes {
			if volume.Type == types.VolumeTypeBind && strings.HasPrefix(volume.Source, prefix) {
				found[volume.Source] = true
			}
		}
	}
	sources := slices.Collect(maps.Keys(found))
	slices.Sort(sources)
	return sources
}

var sharedFolders = []string{"downloads", "media/movies", "media/tvshows"}

func DataFolders(project *types.Project, under string) []string {
	folders := BindSources(project, under)
	shared := filepath.Join(under, "data")
	if !slices.Contains(folders, shared) {
		return folders
	}
	for _, folder := range sharedFolders {
		if inside := filepath.Join(shared, folder); !slices.Contains(folders, inside) {
			folders = append(folders, inside)
		}
	}
	slices.Sort(folders)
	return folders
}

func (r *Runner) RunOnce(ctx context.Context, project *types.Project, service string, out io.Writer) (int, error) {
	alone, err := project.WithSelectedServices([]string{service}, types.IgnoreDependencies)
	if err != nil {
		return 0, err
	}
	if err := r.service.Create(ctx, alone, api.CreateOptions{Services: []string{service}, Recreate: api.RecreateForce, RecreateDependencies: api.RecreateNever}); err != nil {
		return 0, err
	}
	created, err := r.service.Ps(ctx, project.Name, api.PsOptions{Project: alone, All: true, Services: []string{service}})
	if err != nil {
		return 0, err
	}
	if len(created) == 0 {
		return 0, fmt.Errorf("no %s container was created", service)
	}
	id := created[0].ID
	defer func() {
		_, _ = r.docker.ContainerRemove(context.WithoutCancel(ctx), id, client.ContainerRemoveOptions{Force: true})
	}()
	if _, err := r.docker.ContainerStart(ctx, id, client.ContainerStartOptions{}); err != nil {
		return 0, err
	}
	return r.follow(ctx, id, out)
}

func (r *Runner) follow(ctx context.Context, id string, out io.Writer) (int, error) {
	logs, err := r.docker.ContainerLogs(ctx, id, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Follow: true})
	if err != nil {
		return 0, err
	}
	copied := make(chan struct{})
	go func() {
		defer close(copied)
		_, _ = stdcopy.StdCopy(out, out, logs)
	}()
	defer func() { _ = logs.Close() }()
	waited := r.docker.ContainerWait(ctx, id, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case exited := <-waited.Result:
		<-copied
		return int(exited.StatusCode), nil
	case err := <-waited.Error:
		return 0, err
	case <-ctx.Done():
		_, _ = r.docker.ContainerStop(context.WithoutCancel(ctx), id, client.ContainerStopOptions{})
		return 0, ctx.Err()
	}
}
