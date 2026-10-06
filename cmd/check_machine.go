package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

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
			"lingering, and the stack's ports. It changes nothing; for each missing piece it prints the command that fixes it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deps.checkMachine(cmd)
		},
	}
}

func (d Dependencies) checkMachine(cmd *cobra.Command) error {
	ports, err := d.portsCheck(cmd)
	if err != nil {
		return err
	}
	report, err := d.checkedMachine(cmd, ports)
	if err != nil || report.Ready() {
		return err
	}
	return errAlreadyReported
}

func (d Dependencies) checkedMachine(cmd *cobra.Command, ports machine.PortsCheck) (machine.Report, error) {
	env, err := d.machineEnv(ports)
	if err != nil {
		return machine.Report{}, err
	}
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintln(out, "Checking this machine...")
	printer := &printedChecks{out: out, painter: paint.Stdout}
	if d.Terminal != nil && d.Terminal() {
		printer.ticker = d.secondTicker
	}
	report := machine.RunEach(cmd.Context(), env, printer)
	_, _ = fmt.Fprint(out, report.Closing())
	return report, nil
}

func (d Dependencies) machineEnv(ports machine.PortsCheck) (machine.Env, error) {
	account, err := d.Account()
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
	homepagePort, note, override := "", "", []byte(nil)
	i, err := d.anyInstallation(cmd)
	switch {
	case errors.Is(err, installation.ErrSeveralInstallations):
		note = severalInstallationsNote
	case errors.Is(err, installation.ErrNoInstallation):
	case err != nil:
		return machine.PortsCheck{}, err
	default:
		homepagePort = i.Settings["HOMEPAGE_PORT"]
		override, _ = os.ReadFile(filepath.Join(i.Config, "compose.override.yml")) //nolint:gosec // the installation's own override
	}
	check, err := d.stackPortsCheck(override, homepagePort)
	check.Note = note
	return check, err
}

func (d Dependencies) stackPortsCheck(override []byte, homepagePort string) (machine.PortsCheck, error) {
	engineFile, err := fs.ReadFile(d.Engine, compose.Stack.EngineFile)
	if err != nil {
		return machine.PortsCheck{}, err
	}
	ports, unreadable, err := machine.StackPorts(engineFile, override, homepagePort)
	if err != nil {
		return machine.PortsCheck{}, err
	}
	return machine.PortsCheck{Ports: ports, Unreadable: unreadable, Project: compose.Stack.Name, Free: d.PortFree, Published: d.Published}, nil
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

type printedChecks struct {
	out     io.Writer
	painter *paint.Painter
	ticker  func() (<-chan time.Time, func())
	stop    chan struct{}
	done    chan struct{}
}

func (c *printedChecks) Started(name string) {
	_, _ = fmt.Fprint(c.out, machine.Begin(name))
	if c.ticker == nil {
		return
	}
	ticks, stopTicking := c.ticker()
	c.stop, c.done = make(chan struct{}), make(chan struct{})
	go func(stop, done chan struct{}) {
		defer close(done)
		defer stopTicking()
		for {
			select {
			case <-ticks:
				_, _ = fmt.Fprint(c.out, ".")
			case <-stop:
				return
			}
		}
	}(c.stop, c.done)
}

func (c *printedChecks) Finished(result machine.Result) {
	if c.stop != nil {
		close(c.stop)
		<-c.done
		c.stop = nil
	}
	_, _ = fmt.Fprint(c.out, machine.End(result, c.painter))
}

func (d Dependencies) secondTicker() (<-chan time.Time, func()) {
	ticker := time.NewTicker(time.Second)
	return ticker.C, ticker.Stop
}
