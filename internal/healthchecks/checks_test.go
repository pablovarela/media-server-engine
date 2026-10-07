package healthchecks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChecksFor(t *testing.T) {
	assert.Equal(t, []Check{
		{Job: "backup", Slug: "gorgon-backup", Cron: "30 4 * * *"}, {Job: "verify", Slug: "gorgon-verify", Cron: "30 5 * * 0"}, {Job: "update", Slug: "gorgon-update", Cron: "0 5 * * *"},
	}, ChecksFor("gorgon", "main", "pi", ""))
	assert.Equal(t, []Check{{Job: "update", Slug: "gorgon-update-pi2", Cron: "0 5 * * *"}}, ChecksFor("gorgon", "secondary", "pi2", ""))
}

func TestTheMediaBackupCheck(t *testing.T) {
	tests := map[string]struct {
		role, mediaCron string
		jobs            []string
	}{
		"main without media":   {role: "main", jobs: []string{"backup", "verify", "update"}},
		"main with media":      {role: "main", mediaCron: "0 1 * * 0", jobs: []string{"backup", "verify", "media-backup", "update"}},
		"secondary with media": {role: "secondary", mediaCron: "0 1 * * 0", jobs: []string{"update"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var jobs []string
			for _, check := range ChecksFor("gorgon", tt.role, "pi", tt.mediaCron) {
				jobs = append(jobs, check.Job)
			}
			assert.Equal(t, tt.jobs, jobs)
		})
	}
}

func TestTheMediaBackupCheckPayload(t *testing.T) {
	checks := ChecksFor("gorgon", "main", "pi", "0 1 * * 0")

	body := payload(checks[2], Facts{Name: "gorgon", MediaRepository: "b2:bucket:restic-media", SSH: "pablo@gorgon.local"})

	assert.Equal(t, "gorgon-media-backup", body["slug"])
	assert.Equal(t, "media-backup", body["tags"])
	assert.Equal(t, "0 1 * * 0", body["schedule"])
	assert.Equal(t, 86400, body["grace"])
	assert.Equal(t, "Weekly backup of gorgon's media (data/media) to Backblaze B2 with restic, followed by a check that reads a sample of it. "+
		"Runs on Sundays at 01:00 on the main machine via the mse-gorgon-media-backup timer (mse backup --media). "+
		"If it fails: ssh pablo@gorgon.local, journalctl --user-unit mse-gorgon-media-backup.service --since -7d -n 200.", body["desc"])
}

func TestSetUp(t *testing.T) {
	facts := Facts{Name: "gorgon", TimeZone: "Europe/London", Repository: "b2:bucket:gorgon", SSH: "pablo@gorgon.local"}
	var bodies []map[string]any
	answers := map[string]int{"gorgon-backup": 201, "gorgon-verify": 400, "gorgon-update": 200}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, ChecksURL, r.URL.String())
		require.Equal(t, "manage-key", r.Header.Get("X-Api-Key"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		bodies = append(bodies, body)
		return &http.Response{StatusCode: answers[body["slug"].(string)], Body: io.NopCloser(strings.NewReader(`{"error":"invalid schedule"}`))}, nil
	})}

	set, failures := Manage{Client: client, URL: ChecksURL, Key: "manage-key"}.SetUp(context.Background(), ChecksFor("gorgon", "main", "pi", ""), facts)

	assert.Equal(t, []string{"gorgon-backup", "gorgon-update"}, set)
	assert.Equal(t, []string{"gorgon-verify: healthchecks.io answered 400: invalid schedule"}, failures)
	require.Len(t, bodies, 3)
	assert.Equal(t, map[string]any{
		"name": "gorgon-backup", "slug": "gorgon-backup", "tags": "backup", "schedule": "30 4 * * *", "tz": "Europe/London",
		"grace": float64(7200), "channels": "*", "unique": []any{"slug"},
		"desc": "Nightly backup of gorgon's app state (libraries, history, users, settings) to Backblaze B2 with restic. Media files are not included. " +
			"Runs daily at 04:30 on the main machine via the mse-gorgon-backup timer (mse backup --apps). " +
			"If it fails: ssh pablo@gorgon.local, journalctl --user-unit mse-gorgon-backup.service --since today; a stale lock is cleared before the next run.",
	}, bodies[0])
	assert.Equal(t, "Weekly check that gorgon's backups in Backblaze B2 can be read back (restic check). "+
		"Runs on Sundays at 05:30 on the main machine via the mse-gorgon-verify timer (mse check-backup). "+
		"If it fails: ssh pablo@gorgon.local, journalctl --user-unit mse-gorgon-verify.service --since -7d -n 200.", bodies[1]["desc"])
	assert.Equal(t, "Daily update of gorgon: pulls the config and the newest compatible mse, pulls images and brings the apps up. "+
		"Runs daily at 05:00 via the mse-gorgon-update timer (mse update --apply). It refuses to run while the config has uncommitted changes. "+
		"If it fails: ssh pablo@gorgon.local, journalctl --user-unit mse-gorgon-update.service --since today, then systemctl --user start mse-gorgon-update.service to retry.", bodies[2]["desc"])
}

func TestSetUpDescribesTheDestination(t *testing.T) {
	for repository, destination := range map[string]string{"/mnt/backups": "a local folder", "s3:host/bucket": "s3 storage", "": "its backup repository"} {
		t.Run(destination, func(t *testing.T) {
			assert.Contains(t, description(Check{Job: "backup", Cron: "30 4 * * *"}, Facts{Name: "gorgon", Repository: repository}), "to "+destination+" with restic")
		})
	}
}

func TestSetUpWithoutATimeZoneUsesUTC(t *testing.T) {
	assert.Equal(t, "Etc/UTC", payload(Check{Job: "update", Slug: "gorgon-update", Cron: "0 5 * * *"}, Facts{Name: "gorgon"})["tz"])
}

func TestSetUpUnreachable(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("no route to host") })}

	set, failures := Manage{Client: client, URL: ChecksURL, Key: "k"}.SetUp(context.Background(), ChecksFor("gorgon", "secondary", "pi2", ""), Facts{Name: "gorgon"})

	assert.Empty(t, set)
	assert.Equal(t, []string{"gorgon-update-pi2: no route to host"}, failures)
}

func TestRetiredChecks(t *testing.T) {
	assert.Equal(t, []string{"gorgon-media-backup"}, RetiredFor("gorgon", "main", ""))
	assert.Empty(t, RetiredFor("gorgon", "main", "0 1 * * 0"))
	assert.Empty(t, RetiredFor("gorgon", "secondary", ""), "the main owns the media check")
}

func TestRetire(t *testing.T) {
	tests := map[string]struct {
		listed            string
		deleteStatus      int
		removed, failures []string
		requests          []string
	}{
		"a check that exists is deleted": {
			listed: `{"checks":[{"slug":"gorgon-media-backup","uuid":"5f1e"}]}`, deleteStatus: 200,
			removed:  []string{"gorgon-media-backup"},
			requests: []string{"GET /api/v3/checks/?slug=gorgon-media-backup", "DELETE /api/v3/checks/5f1e"},
		},
		"no such check": {
			listed:   `{"checks":[]}`,
			requests: []string{"GET /api/v3/checks/?slug=gorgon-media-backup"},
		},
		"a refused delete": {
			listed: `{"checks":[{"slug":"gorgon-media-backup","uuid":"5f1e"}]}`, deleteStatus: 403,
			failures: []string{"gorgon-media-backup: healthchecks.io answered 403"},
			requests: []string{"GET /api/v3/checks/?slug=gorgon-media-backup", "DELETE /api/v3/checks/5f1e"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var requests []string
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				require.Equal(t, "manage-key", r.Header.Get("X-Api-Key"))
				requests = append(requests, r.Method+" "+r.URL.RequestURI())
				if r.Method == http.MethodGet {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tt.listed))}, nil
				}
				return &http.Response{StatusCode: tt.deleteStatus, Body: io.NopCloser(strings.NewReader("{}"))}, nil
			})}

			removed, failures := Manage{Client: client, URL: ChecksURL, Key: "manage-key"}.Retire(context.Background(), []string{"gorgon-media-backup"})

			assert.Equal(t, tt.removed, removed)
			assert.Equal(t, tt.failures, failures)
			assert.Equal(t, tt.requests, requests)
		})
	}
}
