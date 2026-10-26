package portforward

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

const (
	gluetunContainer = "gluetun"
	portForwardURL   = "http://127.0.0.1:8000/v1/portforward"
)

var (
	ErrNoPort         = errors.New("the VPN has no forwarded port; downloads continue without incoming connections")
	ErrGluetun        = errors.New("gluetun isn't answering")
	ErrGluetunStopped = errors.New("gluetun isn't running")
)

type Docker interface {
	Exec(ctx context.Context, container, user string, command []string) (string, error)
}

type Gluetun struct {
	Docker Docker
	Key    string
}

func (g Gluetun) Port(ctx context.Context) (int, error) {
	output, err := g.Docker.Exec(ctx, gluetunContainer, "", []string{"wget", "-qO-", "--header", "X-API-Key: " + g.Key, portForwardURL})
	if err != nil {
		if strings.Contains(err.Error(), "is not running") {
			return 0, ErrGluetunStopped
		}
		return 0, ErrGluetun
	}
	var answer struct {
		Port  int   `json:"port"`
		Ports []int `json:"ports"`
	}
	if err := json.Unmarshal([]byte(output), &answer); err != nil {
		return 0, ErrGluetun
	}
	if answer.Port == 0 && len(answer.Ports) > 0 {
		answer.Port = answer.Ports[0]
	}
	if answer.Port < 1 || answer.Port > 65535 {
		return 0, ErrNoPort
	}
	return answer.Port, nil
}
