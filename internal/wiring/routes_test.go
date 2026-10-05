package wiring

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type answer struct {
	status int
	body   string
	err    error
}

func ok(body string) answer { return answer{body: body} }

func failing(status int, body string) answer { return answer{status: status, body: body} }

type sent struct {
	method  string
	host    string
	path    string
	body    string
	headers http.Header
}

type routes struct {
	t        *testing.T
	mu       sync.Mutex
	answers  map[string][]answer
	requests []sent
}

func newRoutes(t *testing.T) *routes {
	return &routes{t: t, answers: map[string][]answer{}}
}

func (r *routes) on(method, address string, answers ...answer) *routes {
	if len(answers) == 0 {
		answers = []answer{ok("")}
	}
	r.answers[method+" "+address] = answers
	return r
}

func (r *routes) client() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		path := request.URL.Path
		if request.URL.RawQuery != "" {
			path += "?" + request.URL.RawQuery
		}
		body := ""
		if request.Body != nil {
			read, err := io.ReadAll(request.Body)
			require.NoError(r.t, err)
			body = string(read)
		}
		r.requests = append(r.requests, sent{method: request.Method, host: request.URL.Host, path: path, body: body, headers: request.Header.Clone()})
		key := request.Method + " " + request.URL.Scheme + "://" + request.URL.Host + path
		outcomes, found := r.answers[key]
		if !found {
			key = request.Method + " " + path
			outcomes, found = r.answers[key]
		}
		require.True(r.t, found, "unexpected request: %s %s%s", request.Method, request.URL.Host, path)
		outcome := outcomes[0]
		if len(outcomes) > 1 {
			r.answers[key] = outcomes[1:]
		}
		if outcome.err != nil {
			return nil, outcome.err
		}
		status := outcome.status
		if status == 0 {
			status = http.StatusOK
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(outcome.body))}, nil
	})}
}

func (r *routes) writes(host ...string) []string {
	var writes []string
	for _, request := range r.to(host) {
		if request.method != http.MethodGet {
			writes = append(writes, request.method+" "+request.path)
		}
	}
	return writes
}

func (r *routes) to(host []string) []sent {
	if len(host) == 0 {
		return r.requests
	}
	var matching []sent
	for _, request := range r.requests {
		if request.host == host[0] {
			matching = append(matching, request)
		}
	}
	return matching
}

func (r *routes) sentBodies(method, path string) []map[string]any {
	var bodies []map[string]any
	for _, request := range r.requests {
		if request.method == method && request.path == path {
			bodies = append(bodies, decoded(r.t, request.body))
		}
	}
	return bodies
}

func (r *routes) sentBody(method, path string) map[string]any {
	bodies := r.sentBodies(method, path)
	require.NotEmpty(r.t, bodies, "no %s %s was sent", method, path)
	return bodies[0]
}

func decoded(t *testing.T, body string) map[string]any {
	t.Helper()
	var value map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &value), body)
	return value
}

func jsonOf(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}
