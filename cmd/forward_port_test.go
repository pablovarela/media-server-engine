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
	assert.Equal(p.t, []string{"cat", "/tmp/gluetun/forwarded_port"}, command)
	return p.output, p.err
}

type forwardingHome struct {
	getenv func(string) string
	home   string
	state  string
}

func newForwardingHome(t *testing.T, settings, apps string) forwardingHome {
	t.Helper()
	getenv, home := xdgHome(t, map[string]string{"home": "INSTALLATION_NAME=home\n" + settings})
	state := filepath.Join(home, ".local", "state", "mse", "home")
	require.NoError(t, os.MkdirAll(filepath.Join(state, ".secrets"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(state, ".secrets", "healthchecks.env"), []byte("HEALTHCHECKS_PING_KEY=ping-key\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(state, ".secrets", "apps.env"), []byte(apps), 0o600))
	data := filepath.Join(home, ".local", "share", "mse", "home")
	require.NoError(t, os.MkdirAll(data, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(data, ".backup-main"), nil, 0o644))
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
		portFile portFile
	}
	type Then struct {
		code   int
		stdout string
		stderr string
		pings  []string
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
			Given: Given{settings: "VPN_PORT_FORWARDING=yes\n", apps: apps, portFile: portFile{output: "51234\n"}},
			Then:  Then{stdout: "moved Deluge to forwarded port 51234\n", pings: []string{start, success}},
		},
		"the VPN has no forwarded port": {
			Given: Given{settings: "VPN_PORT_FORWARDING=yes\n", apps: apps, portFile: portFile{err: errors.New("cat: can't open '/tmp/gluetun/forwarded_port': No such file or directory")}},
			Then:  Then{code: 1, stderr: "the VPN has no forwarded port; downloads continue without incoming connections", pings: []string{start, failure}},
		},
		"no Deluge password": {
			Given: Given{settings: "VPN_PORT_FORWARDING=yes\n", portFile: portFile{output: "51234\n"}},
			Then:  Then{code: 1, stderr: "DELUGE_WEB_PASSWORD is not in the app secrets", pings: []string{start, failure}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			h := newForwardingHome(t, tt.Given.settings, tt.Given.apps)
			var pings []string
			tt.Given.portFile.t = t
			root := NewRootCommand(Dependencies{
				Environment: h.getenv, Home: h.home, Update: newMockUpdater(t), Host: thisHost, HTTP: delugeOnPort(t, 6881, &pings),
				Docker: func() (portforward.Docker, error) { return tt.Given.portFile, nil },
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
				assert.Equal(t, tt.Then.code != 0, record.Failed)
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
		return "", errors.New("cat: can't open '/tmp/gluetun/forwarded_port': No such file or directory")
	}
	return answer, nil
}

func forwardingInstallation(t *testing.T, settings map[string]string, marked bool) *installation.Installation {
	t.Helper()
	root := t.TempDir()
	i := &installation.Installation{Name: "home", Settings: settings, Data: filepath.Join(root, "data"), State: filepath.Join(root, "state")}
	require.NoError(t, os.MkdirAll(filepath.Join(i.State, ".secrets"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(i.State, ".secrets", "apps.env"), []byte("DELUGE_WEB_PASSWORD=pw\n"), 0o600))
	if marked {
		require.NoError(t, wiring.State{Dir: filepath.Join(i.Data, "volumes", ".wiring")}.Remember(portforward.Marker, "yes"))
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
