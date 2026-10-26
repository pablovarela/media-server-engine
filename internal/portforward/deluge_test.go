package portforward

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type call struct {
	method string
	params string
	answer string
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func delugeAnswering(t *testing.T, calls ...call) http.RoundTripper {
	t.Helper()
	next := 0
	t.Cleanup(func() { assert.Equal(t, len(calls), next, "requests made") })
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Less(t, next, len(calls), "unexpected request")
		want := calls[next]
		next++
		assert.Equal(t, "http://localhost:8112/json", r.URL.String())
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var sent struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&sent))
		assert.Equal(t, want.method, sent.Method)
		assert.JSONEq(t, want.params, string(sent.Params))
		header := http.Header{}
		if sent.Method == "auth.login" {
			header.Set("Set-Cookie", "_session_id=s1; Path=/json")
		} else {
			cookie, err := r.Cookie("_session_id")
			require.NoError(t, err, "session cookie on "+sent.Method)
			assert.Equal(t, "s1", cookie.Value)
		}
		if want.answer == "" {
			return nil, fmt.Errorf("connection refused")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(want.answer)), Request: r}, nil
	})
}

var (
	login     = call{method: "auth.login", params: `["pw"]`, answer: `{"result":true,"error":null,"id":1}`}
	connected = call{method: "web.connected", params: `[]`, answer: `{"result":true,"error":null,"id":2}`}
)

func TestDelugeListen(t *testing.T) {
	setPort := call{method: "core.set_config", params: `[{"random_port":false,"listen_ports":[51234,51234]}]`, answer: `{"result":null,"error":null,"id":3}`}
	tests := map[string]struct {
		calls []call
		err   error
	}{
		"the web UI is attached to the daemon": {calls: []call{login, connected, setPort}},
		"the web UI is not attached yet": {calls: []call{
			login,
			{method: "web.connected", params: `[]`, answer: `{"result":false,"error":null,"id":2}`},
			{method: "web.get_hosts", params: `[]`, answer: `{"result":[["id1","127.0.0.1",58846,"localclient"]],"error":null,"id":3}`},
			{method: "web.connect", params: `["id1"]`, answer: `{"result":[],"error":null,"id":4}`},
			setPort,
		}},
		"a wrong password": {calls: []call{{method: "auth.login", params: `["pw"]`, answer: `{"result":false,"error":null,"id":1}`}}, err: ErrDelugeLogin},
		"Deluge is down":   {calls: []call{{method: "auth.login", params: `["pw"]`}}, err: ErrDeluge},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			d := NewDeluge("http://localhost:8112", "pw", delugeAnswering(t, tt.calls...))

			err := d.Listen(context.Background(), 51234)

			if tt.err == nil {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tt.err)
			assert.NotContains(t, err.Error(), "pw")
		})
	}
}

func TestDelugeLogsInOnce(t *testing.T) {
	d := NewDeluge("http://localhost:8112", "pw", delugeAnswering(t,
		login, connected,
		call{method: "core.get_listen_port", params: `[]`, answer: `{"result":6881,"error":null,"id":3}`},
		call{method: "core.set_config", params: `[{"random_port":true}]`, answer: `{"result":null,"error":null,"id":4}`},
	))

	port, err := d.ListenPort(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 6881, port)
	assert.NoError(t, d.RandomPort(context.Background()))
}

func TestDelugeReportsAnRPCError(t *testing.T) {
	d := NewDeluge("http://localhost:8112", "pw", delugeAnswering(t,
		login, connected,
		call{method: "core.get_listen_port", params: `[]`, answer: `{"result":null,"error":{"message":"Not authenticated","code":1},"id":3}`},
	))

	_, err := d.ListenPort(context.Background())

	assert.EqualError(t, err, "the Deluge web UI answered core.get_listen_port with: Not authenticated")
}
