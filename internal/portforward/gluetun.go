package portforward

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	gluetunContainer = "gluetun"
	portFile         = "/tmp/gluetun/forwarded_port"
)

var (
	ErrNoPort  = errors.New("the VPN has no forwarded port; downloads continue without incoming connections")
	ErrGluetun = errors.New("gluetun isn't answering")
)

type Docker interface {
	Exec(ctx context.Context, container, user string, command []string) (string, error)
}

type Gluetun struct {
	Docker Docker
}

func (g Gluetun) Port(ctx context.Context) (int, error) {
	output, err := g.Docker.Exec(ctx, gluetunContainer, "", []string{"cat", portFile})
	if err != nil {
		if strings.Contains(err.Error(), "No such file") {
			return 0, ErrNoPort
		}
		return 0, fmt.Errorf("%w: %v", ErrGluetun, err)
	}
	port, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil || port < 1 || port > 65535 {
		return 0, ErrNoPort
	}
	return port, nil
}
