package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/apply"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/jobs"
	"github.com/pablovarela/media-server-engine/internal/portforward"
	"github.com/pablovarela/media-server-engine/internal/secrets"
	"github.com/pablovarela/media-server-engine/internal/wiring"
)

func newForwardPortCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "forward-port",
		Short: "Keep Deluge listening on the port the VPN forwards",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			i, err := deps.installation(cmd)
			if err != nil {
				return err
			}
			enabled, err := portforward.Enabled(i.Settings)
			if err != nil {
				return err
			}
			if !enabled {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "port forwarding is off ("+portforward.Setting+")")
				return err
			}
			pings := deps.pinger(cmd, i)
			if healthchecksKey(i, "HEALTHCHECKS_PING_KEY") == "" {
				pings.pings.ErrOut = io.Discard
			}
			finishing := context.WithoutCancel(cmd.Context())
			pings.Ping(cmd.Context(), jobs.ForwardPort, "/start")
			result, err := deps.waitForPort(cmd.Context(), i)
			defer shieldSignals()()
			switch {
			case errors.Is(err, portforward.ErrGluetunStopped):
				// The backups stop the stack; without a ping healthchecks.io calls the check down only if gluetun stays stopped past its grace.
				pings.runs.finish(jobs.ForwardPort, false)
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "gluetun isn't running; nothing to check")
				return err
			case err != nil:
				pings.Ping(finishing, jobs.ForwardPort, "/fail")
				return err
			}
			pings.Ping(finishing, jobs.ForwardPort, "")
			_, err = fmt.Fprintln(cmd.OutOrStdout(), result)
			return err
		},
	}
}

const (
	portWait  = 60 * time.Second
	portRetry = 5 * time.Second
)

func (d Dependencies) forwarding(i *installation.Installation) apply.Forwarding {
	enabled, err := portforward.Enabled(i.Settings)
	switch {
	case err != nil:
		return forwardingFunc(func(context.Context) (string, error) { return "", err })
	case enabled:
		return forwardingFunc(func(ctx context.Context) (string, error) { return d.waitForPort(ctx, i) })
	case wiringState(i).Remembered(portforward.Marker) != "":
		return forwardingFunc(func(ctx context.Context) (string, error) { return d.restorePort(ctx, i) })
	}
	return nil
}

func (d Dependencies) waitForPort(ctx context.Context, i *installation.Installation) (string, error) {
	sync, err := d.portSync(i)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, portWait)
	defer cancel()
	for retries := 0; ; retries++ {
		result, err := sync.Forward(ctx)
		if !startingUp(err) || retries == int(portWait/portRetry) {
			if err != nil {
				return "", err
			}
			return result.String(), nil
		}
		if d.Pause(ctx, portRetry) != nil {
			return "", err
		}
	}
}

func startingUp(err error) bool {
	for _, passing := range []error{portforward.ErrNoPort, portforward.ErrGluetun, portforward.ErrDeluge, portforward.ErrNotMoved} {
		if errors.Is(err, passing) {
			return true
		}
	}
	return false
}

func (d Dependencies) restorePort(ctx context.Context, i *installation.Installation) (string, error) {
	if declaresDelugeListenPort(i) {
		return "Deluge on the port apps.yml declares", wiringState(i).Remember(portforward.Marker, "")
	}
	deluge, err := d.deluge(i)
	if err != nil {
		return "", err
	}
	restored, err := portforward.Sync{Deluge: deluge, State: wiringState(i)}.Restore(ctx)
	if err != nil || !restored {
		return "nothing to restore", err
	}
	return "Deluge back on a random port", nil
}

func declaresDelugeListenPort(i *installation.Installation) bool {
	if i.Config == "" {
		return false
	}
	declared, err := wiring.Env{Config: i.Config}.Declared("apps.yml")
	if err != nil {
		return false
	}
	deluge, _ := declared["deluge"].(map[string]any)
	core, _ := deluge["core"].(map[string]any)
	_, random := core["random_port"]
	_, ports := core["listen_ports"]
	return random || ports
}

type forwardingFunc func(context.Context) (string, error)

func (f forwardingFunc) Sync(ctx context.Context) (string, error) { return f(ctx) }

func wiringState(i *installation.Installation) wiring.State {
	return wiring.State{Dir: filepath.Join(i.Data, "volumes", ".wiring")}
}

func (d Dependencies) deluge(i *installation.Installation) (*portforward.Deluge, error) {
	password := secrets.Dotenv(readSecretsFile(i, "apps.env"))["DELUGE_WEB_PASSWORD"]
	if password == "" {
		return nil, errors.New("DELUGE_WEB_PASSWORD is not in the app secrets")
	}
	url := wiring.Env{Settings: i.Settings}.URL("DELUGE_URL", "http://localhost:8112")
	return portforward.NewDeluge(url, password, d.HTTP.Transport), nil
}

func (d Dependencies) portSync(i *installation.Installation) (portforward.Sync, error) {
	deluge, err := d.deluge(i)
	if err != nil {
		return portforward.Sync{}, err
	}
	key := wiringState(i).Remembered("gluetun-control.key")
	if key == "" {
		return portforward.Sync{}, errors.New("gluetun's control key is missing; mse apply creates it")
	}
	docker, err := d.Docker()
	if err != nil {
		return portforward.Sync{}, err
	}
	return portforward.Sync{Gluetun: portforward.Gluetun{Docker: docker, Key: key}, Deluge: deluge, State: wiringState(i)}, nil
}
