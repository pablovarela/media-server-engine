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
	for waited := time.Duration(0); ; waited += portRetry {
		result, err := sync.Forward(ctx)
		if !startingUp(err) || waited >= portWait {
			if err != nil {
				return "", err
			}
			return result.String(), nil
		}
		if err := d.Pause(ctx, portRetry); err != nil {
			return "", err
		}
	}
}

func startingUp(err error) bool {
	return errors.Is(err, portforward.ErrNoPort) || errors.Is(err, portforward.ErrGluetun) || errors.Is(err, portforward.ErrDeluge)
}

func (d Dependencies) restorePort(ctx context.Context, i *installation.Installation) (string, error) {
	password := secrets.Dotenv(readSecretsFile(i, "apps.env"))["DELUGE_WEB_PASSWORD"]
	sync := portforward.Sync{Deluge: portforward.NewDeluge(delugeURL(i), password, d.HTTP.Transport), State: wiringState(i)}
	restored, err := sync.Restore(ctx)
	if err != nil || !restored {
		return "nothing to restore", err
	}
	return "Deluge back on a random port", nil
}

type forwardingFunc func(context.Context) (string, error)

func (f forwardingFunc) Sync(ctx context.Context) (string, error) { return f(ctx) }

func wiringState(i *installation.Installation) wiring.State {
	return wiring.State{Dir: filepath.Join(i.Data, "volumes", ".wiring")}
}

func delugeURL(i *installation.Installation) string {
	return wiring.Env{Settings: i.Settings}.URL("DELUGE_URL", "http://localhost:8112")
}

func (d Dependencies) portSync(i *installation.Installation) (portforward.Sync, error) {
	password := secrets.Dotenv(readSecretsFile(i, "apps.env"))["DELUGE_WEB_PASSWORD"]
	if password == "" {
		return portforward.Sync{}, errors.New("DELUGE_WEB_PASSWORD is not in the app secrets")
	}
	key := wiringState(i).Remembered("gluetun-control.key")
	if key == "" {
		return portforward.Sync{}, errors.New("gluetun's control key is missing; mse apply creates it")
	}
	docker, err := d.Docker()
	if err != nil {
		return portforward.Sync{}, err
	}
	return portforward.Sync{
		Gluetun: portforward.Gluetun{Docker: docker, Key: key},
		Deluge:  portforward.NewDeluge(delugeURL(i), password, d.HTTP.Transport),
		State:   wiringState(i),
	}, nil
}
