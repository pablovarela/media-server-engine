package portforward

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"time"
)

const requestTimeout = 30 * time.Second

var (
	ErrDeluge      = errors.New("the Deluge web UI isn't answering")
	ErrDelugeLogin = errors.New("the Deluge web UI refuses the web password")
)

type Deluge struct {
	client   *http.Client
	url      string
	password string
	ready    bool
	id       int
}

func NewDeluge(url, password string, transport http.RoundTripper) *Deluge {
	jar, _ := cookiejar.New(nil)
	return &Deluge{client: &http.Client{Transport: transport, Jar: jar}, url: url, password: password}
}

func (d *Deluge) ListenPort(ctx context.Context) (int, error) {
	var port int
	return port, d.session(ctx, "core.get_listen_port", []any{}, &port)
}

func (d *Deluge) Listen(ctx context.Context, port int) error {
	return d.session(ctx, "core.set_config", []any{map[string]any{"random_port": false, "listen_ports": []int{port, port}}}, nil)
}

func (d *Deluge) RandomPort(ctx context.Context) error {
	return d.session(ctx, "core.set_config", []any{map[string]any{"random_port": true}}, nil)
}

func (d *Deluge) session(ctx context.Context, method string, params []any, into any) error {
	if !d.ready {
		if err := d.attach(ctx); err != nil {
			return err
		}
		d.ready = true
	}
	err := d.call(ctx, method, params, into)
	if err != nil {
		d.ready = false
	}
	return err
}

func (d *Deluge) attach(ctx context.Context) error {
	var loggedIn bool
	if err := d.call(ctx, "auth.login", []any{d.password}, &loggedIn); err != nil {
		return err
	}
	if !loggedIn {
		return ErrDelugeLogin
	}
	var connected bool
	if err := d.call(ctx, "web.connected", []any{}, &connected); err != nil || connected {
		return err
	}
	var hosts [][]any
	if err := d.call(ctx, "web.get_hosts", []any{}, &hosts); err != nil {
		return err
	}
	if len(hosts) == 0 || len(hosts[0]) == 0 {
		return fmt.Errorf("%w: it lists no daemon to connect to", ErrDeluge)
	}
	return d.call(ctx, "web.connect", []any{hosts[0][0]}, nil)
}

func (d *Deluge) call(ctx context.Context, method string, params []any, into any) error {
	d.id++
	body, err := json.Marshal(map[string]any{"method": method, "params": params, "id": d.id})
	if err != nil {
		return err
	}
	timed, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(timed, http.MethodPost, d.url+"/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := d.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrDeluge
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w (HTTP %d)", ErrDeluge, response.StatusCode)
	}
	var answer struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&answer); err != nil {
		return fmt.Errorf("%w: unreadable answer to %s", ErrDeluge, method)
	}
	if answer.Error != nil {
		return fmt.Errorf("%w: %s: %s", ErrDeluge, method, answer.Error.Message)
	}
	if into == nil {
		return nil
	}
	return json.Unmarshal(answer.Result, into)
}
