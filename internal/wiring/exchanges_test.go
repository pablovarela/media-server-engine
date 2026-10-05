package wiring

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type exchange struct {
	method, url string
	body        string
	header      map[string]string
	status      int
	answer      string
	err         error
}

func exchanges(t *testing.T, want ...exchange) *http.Client {
	t.Helper()
	next := 0
	t.Cleanup(func() { assert.Equal(t, len(want), next, "every expected request was made") })
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Less(t, next, len(want), "unexpected %s %s", r.Method, r.URL)
		e := want[next]
		next++
		assert.Equal(t, e.method+" "+e.url, r.Method+" "+r.URL.String())
		for k, v := range e.header {
			assert.Equal(t, v, r.Header.Get(k), k)
		}
		assertBody(t, e.body, r)
		if e.err != nil {
			return nil, e.err
		}
		status := e.status
		if status == 0 {
			status = http.StatusOK
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(e.answer))}, nil
	})}
}

func assertBody(t *testing.T, want string, r *http.Request) {
	t.Helper()
	got := ""
	if r.Body != nil {
		read, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		got = string(read)
	}
	switch {
	case want == "":
		assert.Empty(t, got, "%s %s sends no body", r.Method, r.URL.Path)
	case strings.HasPrefix(want, "{") || strings.HasPrefix(want, "["):
		assert.JSONEq(t, want, got, "%s %s", r.Method, r.URL.Path)
	default:
		wanted, err := url.ParseQuery(want)
		require.NoError(t, err)
		sent, err := url.ParseQuery(got)
		require.NoError(t, err)
		assert.Equal(t, wanted, sent, "%s %s", r.Method, r.URL.Path)
	}
}

type said struct {
	lines []string
}

func (s *said) say(line string) { s.lines = append(s.lines, line) }

func testEnv(t *testing.T, client *http.Client, secrets map[string]string) (Env, *said) {
	t.Helper()
	out := &said{}
	return Env{
		Settings: map[string]string{},
		Secrets:  secrets,
		Config:   t.TempDir(),
		Data:     t.TempDir(),
		HTTP:     client,
		Pause:    func(context.Context, time.Duration) error { return nil },
		Say:      out.say,
		Redact:   NewRedactor(secrets),
		changes:  new(int),
	}, out
}
