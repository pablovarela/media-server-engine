package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

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
			result, err := deps.forwardPort(cmd.Context(), i)
			defer shieldSignals()()
			if err != nil {
				pings.Ping(finishing, jobs.ForwardPort, "/fail")
				return err
			}
			pings.Ping(finishing, jobs.ForwardPort, "")
			_, err = fmt.Fprintln(cmd.OutOrStdout(), result)
			return err
		},
	}
}

func (d Dependencies) forwardPort(ctx context.Context, i *installation.Installation) (portforward.Result, error) {
	sync, err := d.portSync(i)
	if err != nil {
		return portforward.Result{}, err
	}
	return sync.Forward(ctx)
}

func (d Dependencies) portSync(i *installation.Installation) (portforward.Sync, error) {
	password := secrets.Dotenv(readSecretsFile(i, "apps.env"))["DELUGE_WEB_PASSWORD"]
	if password == "" {
		return portforward.Sync{}, errors.New("DELUGE_WEB_PASSWORD is not in the app secrets")
	}
	docker, err := d.Docker()
	if err != nil {
		return portforward.Sync{}, err
	}
	deluge := wiring.Env{Settings: i.Settings}.URL("DELUGE_URL", "http://localhost:8112")
	return portforward.Sync{
		Gluetun: portforward.Gluetun{Docker: docker},
		Deluge:  portforward.NewDeluge(deluge, password, d.HTTP.Transport),
		State:   wiring.State{Dir: filepath.Join(i.Data, "volumes", ".wiring")},
	}, nil
}
