package portforward

import (
	"context"
	"errors"
	"fmt"
)

const Marker = "deluge-port-forwarded"

var ErrNotMoved = errors.New("Deluge didn't take the forwarded port")

type Ports interface {
	Port(ctx context.Context) (int, error)
}

type Listener interface {
	ListenPort(ctx context.Context) (int, error)
	Listen(ctx context.Context, port int) error
	RandomPort(ctx context.Context) error
}

type Memory interface {
	Remembered(name string) string
	Remember(name, value string) error
}

type Sync struct {
	Gluetun Ports
	Deluge  Listener
	State   Memory
}

type Result struct {
	Port  int
	Moved bool
}

func (r Result) String() string {
	if r.Moved {
		return fmt.Sprintf("moved Deluge to forwarded port %d", r.Port)
	}
	return fmt.Sprintf("Deluge listens on forwarded port %d", r.Port)
}

func (s Sync) Forward(ctx context.Context) (Result, error) {
	port, err := s.Gluetun.Port(ctx)
	if err != nil {
		return Result{}, err
	}
	current, err := s.Deluge.ListenPort(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := s.State.Remember(Marker, yes); err != nil {
		return Result{}, err
	}
	if current == port {
		return Result{Port: port}, nil
	}
	if err := s.Deluge.Listen(ctx, port); err != nil {
		return Result{}, err
	}
	if current, err = s.Deluge.ListenPort(ctx); err != nil {
		return Result{}, err
	}
	if current != port {
		return Result{}, fmt.Errorf("%w: it listens on %d, not %d", ErrNotMoved, current, port)
	}
	return Result{Port: port, Moved: true}, nil
}

func (s Sync) Restore(ctx context.Context) (bool, error) {
	if s.State.Remembered(Marker) != yes {
		return false, nil
	}
	if err := s.Deluge.RandomPort(ctx); err != nil {
		return false, err
	}
	return true, s.State.Remember(Marker, "")
}
