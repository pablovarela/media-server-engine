package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/paint"
	"github.com/pablovarela/media-server-engine/internal/report"
	"github.com/pablovarela/media-server-engine/internal/secrets"
)

type composeRunner interface {
	Load(ctx context.Context, i *installation.Installation, kind compose.Kind, variables, profiles []string) (*types.Project, error)
	Up(ctx context.Context, project *types.Project, services []string, wait compose.Wait) error
	Down(ctx context.Context, project *types.Project) error
	Ps(ctx context.Context, project *types.Project) ([]compose.Container, error)
	Logs(ctx context.Context, project *types.Project, options compose.LogsOptions, w io.Writer) error
	Restart(ctx context.Context, project *types.Project, services []string) error
	RunningServices(ctx context.Context, project *types.Project) ([]string, error)
	AnyRunning(ctx context.Context, project *types.Project) (bool, error)
	Stop(ctx context.Context, project *types.Project) error
	Start(ctx context.Context, project *types.Project, services []string) error
	Pull(ctx context.Context, project *types.Project) (compose.Pulled, error)
	Recreate(ctx context.Context, project *types.Project, services []string) error
	Detached(ctx context.Context, project *types.Project, from string, dependents []string) ([]string, error)
	RunOnce(ctx context.Context, project *types.Project, service string, out io.Writer) (int, error)
}

type projectOperation func(cmd *cobra.Command, o opened, args []string) error

type drawing int

const (
	noDrawing drawing = iota
	drawingWhenPinned
	drawingAlways
)

type opened struct {
	installation *installation.Installation
	network      string
	runner       composeRunner
	project      *types.Project
	page         *pageChanges
	outcomes     *compose.Outcomes
}

func (d Dependencies) openProject(cmd *cobra.Command, kind compose.Kind, draw drawing) (opened, error) {
	i, err := d.installation(cmd)
	if err != nil {
		return opened{}, err
	}
	if err := secrets.WriteAll(i, d.Decrypt, secrets.RandomKey); err != nil {
		return opened{}, err
	}
	if err := compose.Prepare(d.Engine, i.State); err != nil {
		return opened{}, err
	}
	network, err := i.NetworkName(d.Host)
	if err != nil {
		return opened{}, err
	}
	profiles, err := compose.Profiles(i, kind, false)
	if err != nil {
		return opened{}, err
	}
	page, err := d.drawPageFor(cmd, i, network, profiles, draw)
	if err != nil {
		return opened{}, err
	}
	outcomes := &compose.Outcomes{}
	runner, err := d.Compose(report.From(cmd.Context()).Tool("compose"), outcomes)
	if err != nil {
		return opened{}, err
	}
	project, err := runner.Load(cmd.Context(), i, kind, compose.Variables(i, network, compose.DockerGID()), profiles)
	return opened{installation: i, network: network, runner: runner, project: project, page: page, outcomes: outcomes}, err
}

func (d Dependencies) drawPageFor(cmd *cobra.Command, i *installation.Installation, network string, profiles []string, draw drawing) (*pageChanges, error) {
	if draw == noDrawing || (draw == drawingWhenPinned && !slices.Contains(profiles, homepageService)) {
		return nil, nil
	}
	r := report.From(cmd.Context())
	step := r.Step("Drawing the landing page")
	changes, err := d.drawPage(cmd.Context(), i, network, cmd.ErrOrStderr())
	switch {
	case err == nil:
		step.Done("done")
		return &changes, nil
	case draw == drawingAlways:
		return nil, step.Fail(err)
	}
	step.Done("failed")
	r.Warn(paint.Stderr.Warning(fmt.Sprintf("could not draw the landing page (%v); it keeps its previous files", err)))
	return nil, nil
}

func composeStep(cmd *cobra.Command, o opened, title string, do func() error) error {
	step := report.From(cmd.Context()).Step(title)
	if err := do(); err != nil {
		return step.Fail(err)
	}
	outcome := o.outcomes.Take()
	if len(outcome.Failed) > 0 {
		return step.Fail(errors.New(strings.Join(outcome.Failed, "; ")))
	}
	step.Done(outcome.String())
	return nil
}

func titled(verb string, kind compose.Kind, services []string) string {
	if len(services) > 0 {
		return verb + " " + strings.Join(services, ", ")
	}
	return verb + " " + kind.Title()
}

func newProjectCommand(deps Dependencies, use, short string, kind compose.Kind) *cobra.Command {
	command := &cobra.Command{Use: use, Short: short}
	operation := func(use, short string, args cobra.PositionalArgs, draw drawing, do projectOperation) *cobra.Command {
		return &cobra.Command{Use: use, Short: short, Args: args, RunE: func(cmd *cobra.Command, args []string) error {
			o, err := deps.openProject(cmd, kind, draw)
			if err != nil {
				return err
			}
			return do(cmd, o, args)
		}}
	}
	command.AddCommand(
		upCommand(deps, use, kind, operation),
		operation("down", "Stop and remove the containers", cobra.NoArgs, noDrawing, func(cmd *cobra.Command, o opened, _ []string) error {
			return composeStep(cmd, o, titled("Stopping", kind, nil), func() error { return o.runner.Down(cmd.Context(), o.project) })
		}),
		operation("restart [service...]", "Restart the containers", cobra.ArbitraryArgs, drawingWhenPinned, func(cmd *cobra.Command, o opened, args []string) error {
			if err := composeStep(cmd, o, titled("Restarting", kind, args), func() error { return o.runner.Restart(cmd.Context(), o.project, args) }); err != nil {
				return err
			}
			return deps.applyPage(cmd.Context(), o)
		}),
		operation("ps", "List the containers", cobra.NoArgs, noDrawing, func(cmd *cobra.Command, o opened, _ []string) error {
			containers, err := o.runner.Ps(cmd.Context(), o.project)
			if err != nil {
				return err
			}
			return printContainers(report.From(cmd.Context()).Data(), containers)
		}),
		logsCommand(deps, use, kind),
	)
	return command
}

func upCommand(deps Dependencies, parent string, kind compose.Kind, operation func(use, short string, args cobra.PositionalArgs, draw drawing, do projectOperation) *cobra.Command) *cobra.Command {
	var wait bool
	var timeout time.Duration
	command := operation("up [service...]", "Create and start the containers", cobra.ArbitraryArgs, drawingWhenPinned, func(cmd *cobra.Command, o opened, args []string) error {
		waiting := compose.NoWait
		if wait {
			waiting = compose.Wait{Enabled: true, Timeout: timeout}
		}
		if err := composeStep(cmd, o, titled("Starting", kind, args), func() error { return o.runner.Up(cmd.Context(), o.project, args, waiting) }); err != nil {
			return err
		}
		return deps.applyPage(cmd.Context(), o)
	})
	command.Example = "  mse " + parent + " up --wait    start every container and wait until they are healthy"
	command.Flags().BoolVar(&wait, "wait", false, "wait until the containers are running, and healthy when they have a healthcheck")
	command.Flags().DurationVar(&timeout, "wait-timeout", 5*time.Minute, "how long --wait waits before failing")
	return command
}

func logsCommand(deps Dependencies, parent string, kind compose.Kind) *cobra.Command {
	var options compose.LogsOptions
	command := &cobra.Command{
		Use:   "logs [service...]",
		Short: "Print the containers' logs",
		Example: "  mse " + parent + " logs -f <service>    follow a service's logs\n" +
			"  mse " + parent + " logs --tail 50       the last 50 lines of every service",
		RunE: func(cmd *cobra.Command, args []string) error {
			o, err := deps.openProject(cmd, kind, noDrawing)
			if err != nil {
				return err
			}
			options.Services = args
			return o.runner.Logs(cmd.Context(), o.project, options, report.From(cmd.Context()).Data())
		},
	}
	command.Flags().BoolVarP(&options.Follow, "follow", "f", false, "keep printing new lines")
	command.Flags().StringVar(&options.Tail, "tail", "all", "number of lines to show from the end")
	return command
}

func printContainers(w io.Writer, containers []compose.Container) error {
	rows := [][]string{{"NAME", "STATE", "HEALTH", "PORTS"}}
	for _, c := range containers {
		rows = append(rows, []string{c.Name, c.State, c.Health, strings.Join(c.Ports, ", ")})
	}
	widths := columnWidths(rows)
	var out strings.Builder
	for r, row := range rows {
		writeRow(&out, row, widths, r > 0)
	}
	_, err := fmt.Fprint(w, out.String())
	return err
}

func columnWidths(rows [][]string) []int {
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for n, cell := range row {
			widths[n] = max(widths[n], len(cell))
		}
	}
	return widths
}

func writeRow(out *strings.Builder, row []string, widths []int, paintStatuses bool) {
	last := len(row) - 1
	for last > 0 && row[last] == "" {
		last--
	}
	for n, cell := range row[:last+1] {
		if paintStatuses && (n == 1 || n == 2) {
			out.WriteString(paintStatus(cell))
		} else {
			out.WriteString(cell)
		}
		if n < last {
			out.WriteString(strings.Repeat(" ", widths[n]-len(cell)+2))
		}
	}
	out.WriteString("\n")
}

func paintStatus(status string) string {
	switch status {
	case "running", "healthy":
		return paint.Stdout.Success(status)
	case "exited", "dead", "unhealthy":
		return paint.Stdout.Failure(status)
	}
	return paint.Stdout.Warning(status)
}
