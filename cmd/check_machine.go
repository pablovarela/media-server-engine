package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/machine"
	"github.com/pablovarela/media-server-engine/internal/paint"
)

const severalInstallationsNote = "several installations here; using the default homepage port 80"

func newCheckMachineCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "check-machine",
		Short: "Check this machine has what an installation needs, and say how to fix what's missing",
		Long: "Check this machine has what an installation needs: git, gh logged in for git, Docker and the docker group, " +
			"restic, lingering, and the stack's ports. It changes nothing; for each missing piece it prints the command that fixes it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deps.checkMachine(cmd)
		},
	}
}

func (d Dependencies) checkMachine(cmd *cobra.Command) error {
	env, err := d.machineEnv(cmd)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintln(out, "Checking this machine...")
	report := machine.Run(cmd.Context(), env)
	_, _ = fmt.Fprint(out, report.Render(paint.Stdout))
	if report.Problems() > 0 {
		return errAlreadyReported
	}
	return nil
}

func (d Dependencies) machineEnv(cmd *cobra.Command) (machine.Env, error) {
	account, err := d.Account()
	if err != nil {
		return machine.Env{}, err
	}
	ports, err := d.portsCheck(cmd)
	if err != nil {
		return machine.Env{}, err
	}
	return machine.Env{
		Runner:   d.Run(io.Discard, io.Discard),
		Account:  account,
		Home:     d.Home,
		GOOS:     d.GOOS,
		Systemd:  d.Systemd != nil && d.Systemd(),
		ProcRoot: d.procRoot(),
		Ports:    ports,
		Carried:  d.carriedVariables(),
		Getenv:   d.Environment,
	}, nil
}

func (d Dependencies) portsCheck(cmd *cobra.Command) (machine.PortsCheck, error) {
	homepagePort, note := "", ""
	i, err := d.anyInstallation(cmd)
	switch {
	case errors.Is(err, installation.ErrSeveralInstallations):
		note = severalInstallationsNote
	case errors.Is(err, installation.ErrNoInstallation):
	case err != nil:
		return machine.PortsCheck{}, err
	default:
		homepagePort = i.Settings["HOMEPAGE_PORT"]
	}
	composeFile, err := fs.ReadFile(d.Engine, compose.Stack.EngineFile)
	if err != nil {
		return machine.PortsCheck{}, err
	}
	ports, err := machine.StackPorts(composeFile, homepagePort)
	if err != nil {
		return machine.PortsCheck{}, err
	}
	return machine.PortsCheck{Ports: ports, Project: compose.Stack.Name, Free: d.PortFree, Published: d.Published, Note: note}, nil
}

func (d Dependencies) carriedVariables() []string {
	var carried []string
	for _, variable := range carriedIntoUnits {
		if value := d.Environment(variable); value != "" {
			carried = append(carried, variable+"="+value)
		}
	}
	return carried
}
