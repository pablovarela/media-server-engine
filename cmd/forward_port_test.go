package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/portforward"
	"github.com/pablovarela/media-server-engine/internal/runs"
	"github.com/pablovarela/media-server-engine/internal/wiring"
)

type portFile struct {
	t      *testing.T
	output string
	err    error
}

func (p portFile) Exec(_ context.Context, container, _ string, command []string) (string, error) {
	assert.Equal(p.t, "gluetun", container)
	assert.Equal(p.t, []string{"wget", "-qO-", "--header", "X-API-Key: control-key", "http://127.0.0.1:8000/v1/portforward"}, command)
	return p.output, p.err
}

type forwardingHome struct {
	getenv func(string) string
	home   string
	state  string
}

func newForwardingHome(t *testing.T, settings, apps string, key bool) forwardingHome {
	t.Helper()
	getenv, home := xdgHome(t, map[string]string{"home": "INSTALLATION_NAME=home\n" + settings})
	state := filepath.Join(home, ".local", "state", "mse", "home")
	require.NoError(t, os.MkdirAll(filepath.Join(state, ".secrets"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(state, ".secrets", "healthchecks.env"), []byte("HEALTHCHECKS_PING_KEY=ping-key\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(state, ".secrets", "apps.env"), []byte(apps), 0o600))
	data := filepath.Join(home, ".local", "share", "mse", "home")
	require.NoError(t, os.MkdirAll(data, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(data, ".backup-main"), nil, 0o644))
	if key {
		require.NoError(t, wiring.State{Dir: filepath.Join(data, "volumes", ".wiring")}.Remember("gluetun-control.key", "control-key"))
	}
	return forwardingHome{getenv: getenv, home: home, state: state}
}

func delugeOnPort(t *testing.T, port int, pings *[]string) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		answer := func(result any) (*http.Response, error) {
			body, err := json.Marshal(map[string]any{"result": result, "error": nil, "id": 1})
			require.NoError(t, err)
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
		}
		switch r.URL.Host {
		case "hc-ping.com":
			*pings = append(*pings, r.URL.Path)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("OK"))}, nil
		case "localhost:8112":
			var sent struct {
				Method string            `json:"method"`
				Params []json.RawMessage `json:"params"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&sent))
			switch sent.Method {
			case "auth.login", "web.connected":
				return answer(true)
			case "core.get_listen_port":
				return answer(port)
			case "core.set_config":
				var set struct {
					ListenPorts []int `json:"listen_ports"`
				}
				require.NoError(t, json.Unmarshal(sent.Params[0], &set))
				if len(set.ListenPorts) > 0 {
					port = set.ListenPorts[0]
				}
				return answer(nil)
			}
		}
		return nil, errors.New("not reachable")
	})}
}

func TestForwardPort(t *testing.T) {
	const apps = "DELUGE_WEB_PASSWORD=pw\n"
	type Given struct {
		settings string
		apps     string
		noKey    bool
		portFile portFile
	}
	type Then struct {
		code   int
		stdout string
		stderr string
		pings  []string
		failed bool
	}
	start, success, failure := "/ping-key/mse-home-forward-port/start", "/ping-key/mse-home-forward-port", "/ping-key/mse-home-forward-port/fail"
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"forwarding is off": {
			Given: Given{apps: apps},
			Then:  Then{stdout: "port forwarding is off (VPN_PORT_FORWARDING)\n"},
		},
		"Deluge moves to the forwarded port": {
			Given: Given{settings: "VPN_PORT_FORWARDING=yes\n", apps: apps, portFile: portFile{output: `{"port":51234,"ports":[51234]}`}},
			Then:  Then{stdout: "moved Deluge to forwarded port 51234\n", pings: []string{start, success}},
		},
		"the VPN has no forwarded port": {
			Given: Given{settings: "VPN_PORT_FORWARDING=yes\n", apps: apps, portFile: portFile{output: `{"port":0,"ports":[]}`}},
			Then:  Then{code: 1, stderr: "the VPN has no forwarded port; downloads continue without incoming connections", pings: []string{start, failure}, failed: true},
		},
		"gluetun is stopped, as during a backup": {
			Given: Given{settings: "VPN_PORT_FORWARDING=yes\n", apps: apps, portFile: portFile{err: errors.New("container 1a2b is not running")}},
			Then:  Then{stdout: "gluetun isn't running; nothing to check\n", pings: []string{start}},
		},
		"no Deluge password": {
			Given: Given{settings: "VPN_PORT_FORWARDING=yes\n", portFile: portFile{output: `{"port":51234}`}},
			Then:  Then{code: 1, stderr: "DELUGE_WEB_PASSWORD is not in the app secrets", pings: []string{start, failure}, failed: true},
		},
		"no gluetun control key": {
			Given: Given{settings: "VPN_PORT_FORWARDING=yes\n", apps: apps, noKey: true},
			Then:  Then{code: 1, stderr: "gluetun's control key is missing; mse apply creates it", pings: []string{start, failure}, failed: true},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			h := newForwardingHome(t, tt.Given.settings, tt.Given.apps, !tt.Given.noKey)
			var pings []string
			tt.Given.portFile.t = t
			root := NewRootCommand(Dependencies{
				Environment: h.getenv, Home: h.home, Update: newMockUpdater(t), Host: thisHost, HTTP: delugeOnPort(t, 6881, &pings),
				Docker: func() (portforward.Docker, error) { return tt.Given.portFile, nil },
				Pause:  func(context.Context, time.Duration) error { return nil },
			})
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			code := run(context.Background(), root, []string{"forward-port"})

			assert.Equal(t, tt.Then.code, code)
			assert.Equal(t, tt.Then.stdout, stdout.String())
			assert.Contains(t, stderr.String(), tt.Then.stderr)
			assert.Equal(t, tt.Then.pings, pings)
			if tt.Then.pings != nil {
				record, recorded, err := runs.Read(filepath.Join(h.state, "runs"), "forward-port")
				require.NoError(t, err)
				require.True(t, recorded, "the run is recorded for mse status")
				assert.Equal(t, tt.Then.failed, record.Failed)
			}
		})
	}
}

type countedPortFile struct {
	answers []string
	reads   int
}

func (c *countedPortFile) Exec(context.Context, string, string, []string) (string, error) {
	answer := c.answers[min(c.reads, len(c.answers)-1)]
	c.reads++
	if answer == "" {
		return `{"port":0,"ports":[]}`, nil
	}
	return `{"port":` + strings.TrimSpace(answer) + `}`, nil
}

func forwardingInstallation(t *testing.T, settings map[string]string, marked bool) *installation.Installation {
	t.Helper()
	root := t.TempDir()
	i := &installation.Installation{Name: "home", Settings: settings, Data: filepath.Join(root, "data"), State: filepath.Join(root, "state")}
	require.NoError(t, os.MkdirAll(filepath.Join(i.State, ".secrets"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(i.State, ".secrets", "apps.env"), []byte("DELUGE_WEB_PASSWORD=pw\n"), 0o600))
	state := wiring.State{Dir: filepath.Join(i.Data, "volumes", ".wiring")}
	require.NoError(t, state.Remember("gluetun-control.key", "control-key"))
	if marked {
		require.NoError(t, state.Remember(portforward.Marker, "yes"))
	}
	return i
}

func TestApplyWaitsForTheForwardedPort(t *testing.T) {
	tests := map[string]struct {
		answers []string
		pauses  int
		result  string
		err     error
	}{
		"the port is there":              {answers: []string{"51234\n"}, result: "moved Deluge to forwarded port 51234"},
		"the port comes on the 3rd read": {answers: []string{"", "", "51234\n"}, pauses: 2, result: "moved Deluge to forwarded port 51234"},
		"the port never comes":           {answers: []string{""}, pauses: 12, err: portforward.ErrNoPort},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var pings []string
			var paused []time.Duration
			ports := &countedPortFile{answers: tt.answers}
			d := Dependencies{
				HTTP:   delugeOnPort(t, 6881, &pings),
				Docker: func() (portforward.Docker, error) { return ports, nil },
				Pause:  func(_ context.Context, d time.Duration) error { paused = append(paused, d); return nil },
			}

			result, err := d.forwarding(forwardingInstallation(t, map[string]string{"VPN_PORT_FORWARDING": "yes"}, false)).Sync(context.Background())

			assert.Equal(t, tt.result, result)
			if tt.err == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tt.err)
			}
			assert.Len(t, paused, tt.pauses)
			for _, p := range paused {
				assert.Equal(t, 5*time.Second, p)
			}
		})
	}
}

func TestApplyPutsDelugeBackOnARandomPortWhenForwardingIsTurnedOff(t *testing.T) {
	var pings []string
	d := Dependencies{HTTP: delugeOnPort(t, 51234, &pings), Docker: func() (portforward.Docker, error) { return nil, errors.New("not needed") }}
	i := forwardingInstallation(t, map[string]string{}, true)

	result, err := d.forwarding(i).Sync(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, "Deluge back on a random port", result)
	assert.Empty(t, wiring.State{Dir: filepath.Join(i.Data, "volumes", ".wiring")}.Remembered(portforward.Marker))
}

func TestApplyHasNoForwardingStepWhileItWasNeverOn(t *testing.T) {
	assert.Nil(t, Dependencies{}.forwarding(forwardingInstallation(t, map[string]string{}, false)))
}

type refusingFirst struct {
	refusals int
	next     http.RoundTripper
}

func (r *refusingFirst) RoundTrip(req *http.Request) (*http.Response, error) {
	if r.refusals > 0 {
		r.refusals--
		return nil, errors.New("connection refused")
	}
	return r.next.RoundTrip(req)
}

func TestApplyWaitsForDelugeToStart(t *testing.T) {
	var pings []string
	var paused []time.Duration
	d := Dependencies{
		HTTP:   &http.Client{Transport: &refusingFirst{refusals: 2, next: delugeOnPort(t, 6881, &pings).Transport}},
		Docker: func() (portforward.Docker, error) { return &countedPortFile{answers: []string{"51234"}}, nil },
		Pause:  func(_ context.Context, d time.Duration) error { paused = append(paused, d); return nil },
	}

	result, err := d.forwarding(forwardingInstallation(t, map[string]string{"VPN_PORT_FORWARDING": "yes"}, false)).Sync(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, "moved Deluge to forwarded port 51234", result)
	assert.Len(t, paused, 2)
}

type ignoringFirstMove struct {
	ignored bool
	next    http.RoundTripper
}

func (i *ignoringFirstMove) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	if !i.ignored && strings.Contains(string(body), `"listen_ports"`) {
		i.ignored = true
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"result":null,"error":null,"id":1}`)), Request: req}, nil
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	return i.next.RoundTrip(req)
}

func TestApplyGivesDelugeAMomentToTakeThePort(t *testing.T) {
	var pings []string
	var paused []time.Duration
	d := Dependencies{
		HTTP:   &http.Client{Transport: &ignoringFirstMove{next: delugeOnPort(t, 6881, &pings).Transport}},
		Docker: func() (portforward.Docker, error) { return &countedPortFile{answers: []string{"51234"}}, nil },
		Pause:  func(_ context.Context, d time.Duration) error { paused = append(paused, d); return nil },
	}

	result, err := d.forwarding(forwardingInstallation(t, map[string]string{"VPN_PORT_FORWARDING": "yes"}, false)).Sync(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, "moved Deluge to forwarded port 51234", result)
	assert.Len(t, paused, 1)
}

type deadlineCheck struct{ t *testing.T }

func (d deadlineCheck) Exec(ctx context.Context, _, _ string, _ []string) (string, error) {
	deadline, set := ctx.Deadline()
	require.True(d.t, set, "the wait has a deadline")
	assert.WithinDuration(d.t, time.Now().Add(time.Minute), deadline, 5*time.Second)
	return `{"port":51234}`, nil
}

func TestApplyWaitsAMinuteAtMostWhateverEachCallTakes(t *testing.T) {
	var pings []string
	d := Dependencies{
		HTTP:   delugeOnPort(t, 51234, &pings),
		Docker: func() (portforward.Docker, error) { return deadlineCheck{t: t}, nil },
		Pause:  func(context.Context, time.Duration) error { return nil },
	}

	_, err := d.forwarding(forwardingInstallation(t, map[string]string{"VPN_PORT_FORWARDING": "yes"}, false)).Sync(context.Background())

	assert.NoError(t, err)
}

func TestTurningForwardingOffLeavesAPortTheConfigDeclares(t *testing.T) {
	var pings []string
	d := Dependencies{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("Deluge isn't called: the wiring already applied the declared port")
		return nil, errors.New("not reachable")
	})}}
	i := forwardingInstallation(t, map[string]string{}, true)
	i.Config = t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(i.Config, "apps.yml"), []byte("deluge:\n  core:\n    random_port: false\n    listen_ports: [6881, 6881]\n"), 0o644))

	result, err := d.forwarding(i).Sync(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, "Deluge on the port apps.yml declares", result)
	assert.Empty(t, wiring.State{Dir: filepath.Join(i.Data, "volumes", ".wiring")}.Remembered(portforward.Marker))
	assert.Empty(t, pings)
}

func TestTurningForwardingOffWithoutADelugePasswordSaysSo(t *testing.T) {
	i := forwardingInstallation(t, map[string]string{}, true)
	require.NoError(t, os.WriteFile(filepath.Join(i.State, ".secrets", "apps.env"), nil, 0o600))

	_, err := Dependencies{}.forwarding(i).Sync(context.Background())

	assert.EqualError(t, err, "DELUGE_WEB_PASSWORD is not in the app secrets")
}
