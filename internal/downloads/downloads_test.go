package downloads

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/installation"
)

type exchange struct {
	method string
	url    string
	status int
	body   string
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func answering(t *testing.T, exchanges ...exchange) *http.Client {
	t.Helper()
	next := 0
	t.Cleanup(func() { assert.Equal(t, len(exchanges), next, "requests made") })
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Less(t, next, len(exchanges), "unexpected %s %s", r.Method, r.URL)
		want := exchanges[next]
		next++
		assert.Equal(t, want.method, r.Method)
		assert.Equal(t, want.url, r.URL.String())
		assert.NotEmpty(t, r.Header.Get("X-Api-Key"))
		if want.status == 0 {
			return nil, fmt.Errorf("connection refused")
		}
		return &http.Response{StatusCode: want.status, Status: fmt.Sprint(want.status), Body: io.NopCloser(strings.NewReader(want.body))}, nil
	})}
}

func withKeys(t *testing.T, sonarr, radarr bool) *installation.Installation {
	t.Helper()
	i := &installation.Installation{Data: t.TempDir()}
	for app, path := range map[string]string{"sonarr": "volumes/sonarr/data/config.xml", "radarr": "volumes/radarr/config/config.xml"} {
		if (app == "sonarr" && sonarr) || (app == "radarr" && radarr) {
			full := filepath.Join(i.Data, path)
			require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
			require.NoError(t, os.WriteFile(full, []byte("<Config><ApiKey>"+app+"-key</ApiKey></Config>"), 0o644))
		}
	}
	return i
}

const flagged = `{"id":%d,"downloadId":"%s","title":"%s","statusMessages":[{"messages":["Found executable file .exe"]}]}`

func TestClean(t *testing.T) {
	sonarrQueue := "http://localhost:8989/api/v3/queue?page=%d&pageSize=2"
	type Given struct {
		exchanges []exchange
		radarr    bool
	}
	type Then struct {
		out, errOut string
		err         string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"pages through and removes flagged ones": {
			Given: Given{exchanges: []exchange{
				{"GET", fmt.Sprintf(sonarrQueue, 1), 200, `{"totalRecords":3,"records":[` + fmt.Sprintf(flagged, 1, "A", "Bad.Show") + `,{"id":2,"title":"Fine","statusMessages":[]}]}`},
				{"GET", fmt.Sprintf(sonarrQueue, 2), 200, `{"totalRecords":3,"records":[{"id":3,"title":"Also.Fine"}]}`},
				{"DELETE", "http://localhost:8989/api/v3/queue/1?removeFromClient=true&blocklist=true&skipRedownload=false", 200, ""},
			}},
			Then: Then{out: "sonarr: checked 3 queued items, 1 flagged as executable\nsonarr: removed and blocklisted Bad.Show\nradarr: queue not reachable, skipped\n"},
		},
		"one removal per download": {
			Given: Given{exchanges: []exchange{
				{"GET", fmt.Sprintf(sonarrQueue, 1), 200, `{"totalRecords":2,"records":[` + fmt.Sprintf(flagged, 1, "A", "Pack.E01") + `,` + fmt.Sprintf(flagged, 2, "A", "Pack.E02") + `]}`},
				{"DELETE", "http://localhost:8989/api/v3/queue/1?removeFromClient=true&blocklist=true&skipRedownload=false", 200, ""},
			}},
			Then: Then{out: "sonarr: checked 2 queued items, 1 flagged as executable\nsonarr: removed and blocklisted Pack.E01\nradarr: queue not reachable, skipped\n"},
		},
		"nothing flagged": {
			Given: Given{exchanges: []exchange{
				{"GET", fmt.Sprintf(sonarrQueue, 1), 200, `{"totalRecords":1,"records":[{"id":3,"title":"Fine"}]}`},
			}},
			Then: Then{out: "sonarr: checked 1 queued item, none flagged as executable\nradarr: queue not reachable, skipped\n"},
		},
		"empty queue": {
			Given: Given{exchanges: []exchange{
				{"GET", fmt.Sprintf(sonarrQueue, 1), 200, `{"totalRecords":0,"records":[]}`},
			}},
			Then: Then{out: "sonarr: the queue is empty\nradarr: queue not reachable, skipped\n"},
		},
		"unreachable queue": {
			Given: Given{exchanges: []exchange{{"GET", fmt.Sprintf(sonarrQueue, 1), 0, ""}}},
			Then:  Then{out: "sonarr: queue not reachable, skipped\nradarr: queue not reachable, skipped\n"},
		},
		"failed removal": {
			Given: Given{exchanges: []exchange{
				{"GET", fmt.Sprintf(sonarrQueue, 1), 200, `{"totalRecords":1,"records":[` + fmt.Sprintf(flagged, 1, "A", "Bad.Show") + `]}`},
				{"DELETE", "http://localhost:8989/api/v3/queue/1?removeFromClient=true&blocklist=true&skipRedownload=false", 500, ""},
			}},
			Then: Then{out: "sonarr: checked 1 queued item, 1 flagged as executable\nradarr: queue not reachable, skipped\n", errOut: "sonarr: could not remove Bad.Show: Sonarr answered 500 Internal Server Error\n", err: "some flagged downloads could not be removed"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var out, errOut bytes.Buffer

			err := Clean(context.Background(), answering(t, tt.Given.exchanges...), withKeys(t, true, tt.Given.radarr), 2, &out, &errOut)

			assert.Equal(t, tt.Then.out, out.String())
			assert.Equal(t, tt.Then.errOut, errOut.String())
			if tt.Then.err == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.Then.err)
			}
		})
	}
}
