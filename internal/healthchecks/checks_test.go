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
		{Job: "update", Slug: "mse-gorgon-update", Cron: "0 5 * * *"}, {Job: "clean-downloads", Slug: "mse-gorgon-clean-downloads", Cron: "*/15 * * * *"},
		{Job: "backup-apps", Slug: "mse-gorgon-backup-apps", Cron: "30 4 * * *"}, {Job: "check-backup", Slug: "mse-gorgon-check-backup", Cron: "30 5 * * 0"},
	}, ChecksFor("gorgon", "main", "pi", ""))
	assert.Equal(t, []Check{
		{Job: "update", Slug: "mse-gorgon-update-pi2", Cron: "0 5 * * *"}, {Job: "clean-downloads", Slug: "mse-gorgon-clean-downloads-pi2", Cron: "*/15 * * * *"},
	}, ChecksFor("gorgon", "secondary", "pi2", ""))
}

func TestTheMediaBackupCheck(t *testing.T) {
	tests := map[string]struct {
		role, mediaCron string
		jobs            []string
	}{
		"main without media":   {role: "main", jobs: []string{"update", "clean-downloads", "backup-apps", "check-backup"}},
		"main with media":      {role: "main", mediaCron: "0 1 * * 0", jobs: []string{"update", "clean-downloads", "backup-apps", "check-backup", "backup-media"}},
		"secondary with media": {role: "secondary", mediaCron: "0 1 * * 0", jobs: []string{"update", "clean-downloads"}},
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

	body := payload(checks[4], Facts{Name: "gorgon", MediaRepository: "b2:bucket:restic-media", SSH: "pablo@gorgon.local"})

	assert.Equal(t, "mse-gorgon-backup-media", body["slug"])
	assert.Equal(t, "backup-media", body["tags"])
	assert.Equal(t, "0 1 * * 0", body["schedule"])
	assert.Equal(t, 86400, body["grace"])
	assert.Equal(t, "Weekly backup of gorgon's media (data/media) to Backblaze B2 with restic, followed by a check that reads a sample of it. "+
		"Runs on Sundays at 01:00 on the main machine via the mse-gorgon-backup-media timer (mse backup --media). "+
		"If it fails: ssh pablo@gorgon.local, journalctl --user-unit mse-gorgon-backup-media.service --since -7d -n 200.", body["desc"])
}

func TestTheCleanDownloadsCheckPayload(t *testing.T) {
	checks := ChecksFor("gorgon", "main", "pi", "")

	body := payload(checks[1], Facts{Name: "gorgon", SSH: "pablo@gorgon.local"})

	assert.Equal(t, "mse-gorgon-clean-downloads", body["slug"])
	assert.Equal(t, "clean-downloads", body["tags"])
	assert.Equal(t, "*/15 * * * *", body["schedule"])
	assert.Equal(t, 1800, body["grace"])
	assert.Contains(t, body["desc"], "Runs every 15 minutes")
	assert.Contains(t, body["desc"], "journalctl --user-unit mse-gorgon-clean-downloads.service")
}

func TestSetUp(t *testing.T) {
	facts := Facts{Name: "gorgon", TimeZone: "Europe/London", Repository: "b2:bucket:gorgon", SSH: "pablo@gorgon.local"}
	var bodies []map[string]any
	answers := map[string]int{"mse-gorgon-backup-apps": 201, "mse-gorgon-check-backup": 400, "mse-gorgon-update": 200, "mse-gorgon-clean-downloads": 200}
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

	assert.Equal(t, []string{"mse-gorgon-update", "mse-gorgon-clean-downloads", "mse-gorgon-backup-apps"}, set)
	assert.Equal(t, []string{"mse-gorgon-check-backup: healthchecks.io answered 400: invalid schedule"}, failures)
	require.Len(t, bodies, 4)
	assert.Equal(t, map[string]any{
		"name": "mse-gorgon-backup-apps", "slug": "mse-gorgon-backup-apps", "tags": "backup-apps", "schedule": "30 4 * * *", "tz": "Europe/London",
		"grace": float64(7200), "channels": "*", "unique": []any{"slug"},
		"desc": "Nightly backup of gorgon's app state (libraries, history, users, settings) to Backblaze B2 with restic. Media files are not included. " +
			"Runs daily at 04:30 on the main machine via the mse-gorgon-backup-apps timer (mse backup --apps). " +
			"If it fails: ssh pablo@gorgon.local, journalctl --user-unit mse-gorgon-backup-apps.service --since today; a stale lock is cleared before the next run.",
	}, bodies[2])
	assert.Equal(t, "Weekly check that gorgon's backups in Backblaze B2 can be read back (restic check). "+
		"Runs on Sundays at 05:30 on the main machine via the mse-gorgon-check-backup timer (mse check-backup). "+
		"If it fails: ssh pablo@gorgon.local, journalctl --user-unit mse-gorgon-check-backup.service --since -7d -n 200.", bodies[3]["desc"])
	assert.Equal(t, "Daily update of gorgon: pulls the config and the newest compatible mse, pulls images and brings the apps up. "+
		"Runs daily at 05:00 via the mse-gorgon-update timer (mse update --apply). It refuses to run while the config has uncommitted changes. "+
		"If it fails: ssh pablo@gorgon.local, journalctl --user-unit mse-gorgon-update.service --since today, then systemctl --user start mse-gorgon-update.service to retry.", bodies[0]["desc"])
}

func TestSetUpDescribesTheDestination(t *testing.T) {
	for repository, destination := range map[string]string{"/mnt/backups": "a local folder", "s3:host/bucket": "s3 storage", "": "its backup repository"} {
		t.Run(destination, func(t *testing.T) {
			assert.Contains(t, description(Check{Job: "backup-apps", Cron: "30 4 * * *"}, Facts{Name: "gorgon", Repository: repository}), "to "+destination+" with restic")
		})
	}
}

func TestSetUpWithoutATimeZoneUsesUTC(t *testing.T) {
	assert.Equal(t, "Etc/UTC", payload(Check{Job: "update", Slug: "mse-gorgon-update", Cron: "0 5 * * *"}, Facts{Name: "gorgon"})["tz"])
}

func TestSetUpUnreachable(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("no route to host") })}

	set, failures := Manage{Client: client, URL: ChecksURL, Key: "k"}.SetUp(context.Background(), ChecksFor("gorgon", "secondary", "pi2", ""), Facts{Name: "gorgon"})

	assert.Empty(t, set)
	assert.Equal(t, []string{"mse-gorgon-update-pi2: no route to host", "mse-gorgon-clean-downloads-pi2: no route to host"}, failures)
}

func TestRetiredChecks(t *testing.T) {
	assert.Equal(t, []string{"mse-gorgon-backup-media"}, RetiredFor("gorgon", "main", ""))
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
			listed: `{"checks":[{"slug":"mse-gorgon-backup-media","uuid":"5f1e"}]}`, deleteStatus: 200,
			removed:  []string{"mse-gorgon-backup-media"},
			requests: []string{"GET /api/v3/checks/?slug=mse-gorgon-backup-media", "DELETE /api/v3/checks/5f1e"},
		},
		"no such check": {
			listed:   `{"checks":[]}`,
			requests: []string{"GET /api/v3/checks/?slug=mse-gorgon-backup-media"},
		},
		"a refused delete": {
			listed: `{"checks":[{"slug":"mse-gorgon-backup-media","uuid":"5f1e"}]}`, deleteStatus: 403,
			failures: []string{"mse-gorgon-backup-media: healthchecks.io answered 403"},
			requests: []string{"GET /api/v3/checks/?slug=mse-gorgon-backup-media", "DELETE /api/v3/checks/5f1e"},
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

			removed, failures := Manage{Client: client, URL: ChecksURL, Key: "manage-key"}.Retire(context.Background(), []string{"mse-gorgon-backup-media"})

			assert.Equal(t, tt.removed, removed)
			assert.Equal(t, tt.failures, failures)
			assert.Equal(t, tt.requests, requests)
		})
	}
}
