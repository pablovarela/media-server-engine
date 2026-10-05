package healthchecks

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSlug(t *testing.T) {
	tests := map[string]struct {
		Given struct{ job, role string }
		Then  struct{ slug string }
	}{
		"backup":                {Given: struct{ job, role string }{"backup", "main"}, Then: struct{ slug string }{"gorgon-backup"}},
		"update on the main":    {Given: struct{ job, role string }{"update", "main"}, Then: struct{ slug string }{"gorgon-update"}},
		"update on a secondary": {Given: struct{ job, role string }{"update", "secondary"}, Then: struct{ slug string }{"gorgon-update-pi2"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.Then.slug, Slug("gorgon", tt.Given.job, tt.Given.role, "pi2"))
		})
	}
}

func TestPings(t *testing.T) {
	address := "https://hc-ping.com/ping-key/gorgon-backup/start?create=1"
	type answer struct {
		status int
		err    error
	}
	type Given struct {
		key     string
		answers []answer
	}
	type Then struct {
		requests int
		sleeps   []time.Duration
		errOut   string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"reported": {
			Given: Given{key: "ping-key", answers: []answer{{status: 200}}},
			Then:  Then{requests: 1},
		},
		"a 503 is retried": {
			Given: Given{key: "ping-key", answers: []answer{{status: 503}, {status: 200}}},
			Then:  Then{requests: 2, sleeps: []time.Duration{time.Second}},
		},
		"a 404 is not retried": {
			Given: Given{key: "ping-key", answers: []answer{{status: 404}}},
			Then:  Then{requests: 1, errOut: "healthchecks: could not report gorgon-backup/start: healthchecks.io answered 404\n"},
		},
		"gives up after three retries": {
			Given: Given{key: "ping-key", answers: []answer{{status: 500}, {status: 500}, {status: 500}, {status: 500}}},
			Then: Then{requests: 4, sleeps: []time.Duration{time.Second, 2 * time.Second, 4 * time.Second},
				errOut: "healthchecks: could not report gorgon-backup/start: healthchecks.io answered 500\n"},
		},
		"network error hides the key": {
			Given: Given{key: "ping-key", answers: []answer{{err: errors.New("no route")}, {err: errors.New("no route")}, {err: errors.New("no route")}, {err: errors.New("no route")}}},
			Then: Then{requests: 4, sleeps: []time.Duration{time.Second, 2 * time.Second, 4 * time.Second},
				errOut: "healthchecks: could not report gorgon-backup/start: no route\n"},
		},
		"no key": {
			Then: Then{errOut: "no healthchecks ping key configured; gorgon-backup is not reported\n"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			requests := 0
			var sleeps []time.Duration
			var errOut bytes.Buffer
			pings := &Pings{
				Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					assert.Equal(t, address, r.URL.String())
					a := tt.Given.answers[requests]
					requests++
					if a.err != nil {
						return nil, a.err
					}
					return &http.Response{StatusCode: a.status, Body: io.NopCloser(strings.NewReader("OK"))}, nil
				})},
				URL: PingURL, Key: tt.Given.key, ErrOut: &errOut,
				Slug:  func(job string) string { return "gorgon-" + job },
				Sleep: func(d time.Duration) { sleeps = append(sleeps, d) },
			}

			pings.Ping(context.Background(), "backup", "/start")

			assert.Equal(t, tt.Then.requests, requests)
			assert.Equal(t, tt.Then.sleeps, sleeps)
			assert.Equal(t, tt.Then.errOut, errOut.String())
			assert.NotContains(t, errOut.String(), "ping-key")
		})
	}
}

func TestPingsWarnOnceWithoutAKey(t *testing.T) {
	var errOut bytes.Buffer
	pings := &Pings{ErrOut: &errOut, Slug: func(job string) string { return "gorgon-" + job }}

	pings.Ping(context.Background(), "backup", "/start")
	pings.Ping(context.Background(), "backup", "")

	assert.Equal(t, "no healthchecks ping key configured; gorgon-backup is not reported\n", errOut.String())
}
