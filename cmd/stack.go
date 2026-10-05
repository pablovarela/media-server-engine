package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/secrets"
)

type composeRunner interface {
	Load(ctx context.Context, i *installation.Installation, kind compose.Kind, variables, profiles []string) (*types.Project, error)
	Up(ctx context.Context, project *types.Project, services []string) error
	Down(ctx context.Context, project *types.Project) error
	Ps(ctx context.Context, project *types.Project) ([]compose.Container, error)
	Logs(ctx context.Context, project *types.Project, options compose.LogsOptions, w io.Writer) error
	Restart(ctx context.Context, project *types.Project, services []string) error
}

type opened struct {
	installation *installation.Installation
	network      string
	runner       composeRunner
	project      *types.Project
}

func (d Dependencies) openProject(cmd *cobra.Command, kind compose.Kind, draw bool) (opened, error) {
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
	if draw && slices.Contains(profiles, homepageService) {
		if _, err := d.drawPage(cmd.Context(), i, network, cmd.ErrOrStderr()); err != nil {
			return opened{}, err
		}
	}
	runner, err := d.Compose(cmd.OutOrStdout(), cmd.ErrOrStderr())
	if err != nil {
		return opened{}, err
	}
	project, err := runner.Load(cmd.Context(), i, kind, compose.Variables(i, network, compose.DockerGID()), profiles)
	return opened{installation: i, network: network, runner: runner, project: project}, err
}

func newProjectCommand(deps Dependencies, use, short string, kind compose.Kind) *cobra.Command {
	command := &cobra.Command{Use: use, Short: short}
	operation := func(use, short string, args cobra.PositionalArgs, draw bool, do func(cmd *cobra.Command, runner composeRunner, project *types.Project, args []string) error) *cobra.Command {
		return &cobra.Command{Use: use, Short: short, Args: args, RunE: func(cmd *cobra.Command, args []string) error {
			o, err := deps.openProject(cmd, kind, draw)
			if err != nil {
				return err
			}
			return do(cmd, o.runner, o.project, args)
		}}
	}
	command.AddCommand(
		operation("up [service...]", "Create and start the containers", cobra.ArbitraryArgs, true, func(cmd *cobra.Command, r composeRunner, p *types.Project, args []string) error {
			return r.Up(cmd.Context(), p, args)
		}),
		operation("down", "Stop and remove the containers", cobra.NoArgs, false, func(cmd *cobra.Command, r composeRunner, p *types.Project, _ []string) error {
			return r.Down(cmd.Context(), p)
		}),
		operation("restart [service...]", "Restart the containers", cobra.ArbitraryArgs, true, func(cmd *cobra.Command, r composeRunner, p *types.Project, args []string) error {
			return r.Restart(cmd.Context(), p, args)
		}),
		operation("ps", "List the containers", cobra.NoArgs, false, func(cmd *cobra.Command, r composeRunner, p *types.Project, _ []string) error {
			containers, err := r.Ps(cmd.Context(), p)
			if err != nil {
				return err
			}
			return printContainers(cmd.OutOrStdout(), containers)
		}),
		logsCommand(deps, kind),
	)
	return command
}

func logsCommand(deps Dependencies, kind compose.Kind) *cobra.Command {
	var options compose.LogsOptions
	command := &cobra.Command{
		Use:   "logs [service...]",
		Short: "Print the containers' logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			o, err := deps.openProject(cmd, kind, false)
			if err != nil {
				return err
			}
			options.Services = args
			return o.runner.Logs(cmd.Context(), o.project, options, cmd.OutOrStdout())
		},
	}
	command.Flags().BoolVarP(&options.Follow, "follow", "f", false, "keep printing new lines")
	command.Flags().StringVar(&options.Tail, "tail", "all", "number of lines to show from the end")
	return command
}

func printContainers(w io.Writer, containers []compose.Container) error {
	var aligned bytes.Buffer
	table := tabwriter.NewWriter(&aligned, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "NAME\tSTATE\tHEALTH\tPORTS")
	for _, c := range containers {
		_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", c.Name, c.State, c.Health, strings.Join(c.Ports, ", "))
	}
	if err := table.Flush(); err != nil {
		return err
	}
	var out strings.Builder
	for _, line := range strings.SplitAfter(aligned.String(), "\n") {
		out.WriteString(strings.TrimRight(line, " \n"))
		if strings.HasSuffix(line, "\n") {
			out.WriteString("\n")
		}
	}
	_, err := fmt.Fprint(w, out.String())
	return err
}
