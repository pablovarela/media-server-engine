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
	assert.Equal(t, []Check{{"backup", "gorgon-backup"}, {"verify", "gorgon-verify"}, {"update", "gorgon-update"}}, ChecksFor("gorgon", "main", "pi"))
	assert.Equal(t, []Check{{"update", "gorgon-update-pi2"}}, ChecksFor("gorgon", "secondary", "pi2"))
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

	set, failures := Manage{Client: client, URL: ChecksURL, Key: "manage-key"}.SetUp(context.Background(), ChecksFor("gorgon", "main", "pi"), facts)

	assert.Equal(t, []string{"gorgon-backup", "gorgon-update"}, set)
	assert.Equal(t, []string{"gorgon-verify: healthchecks.io answered 400: invalid schedule"}, failures)
	require.Len(t, bodies, 3)
	assert.Equal(t, map[string]any{
		"name": "gorgon-backup", "slug": "gorgon-backup", "tags": "backup", "schedule": "30 4 * * *", "tz": "Europe/London",
		"grace": float64(7200), "channels": "*", "unique": []any{"slug"},
		"desc": "Nightly backup of gorgon's app state (libraries, history, users, settings) to Backblaze B2 with restic. Media files are not included. " +
			"Runs daily at 04:30 on the main machine via the mse-gorgon-backup timer (mse backup). " +
			"If it fails: ssh pablo@gorgon.local, journalctl --user-unit mse-gorgon-backup.service --since today; a stale lock is cleared before the next run.",
	}, bodies[0])
	assert.Equal(t, "Weekly check that gorgon's backups in Backblaze B2 can be read back (restic check). "+
		"Runs on Sundays at 05:30 on the main machine via the mse-gorgon-verify timer (mse verify-backup). "+
		"If it fails: ssh pablo@gorgon.local, journalctl --user-unit mse-gorgon-verify.service --since -7d -n 200.", bodies[1]["desc"])
	assert.Equal(t, "Daily update of gorgon: pulls the config and the newest compatible mse, pulls images and brings the apps up. "+
		"Runs daily at 05:00 via the mse-gorgon-update timer (mse update --apply). It refuses to run while the config has uncommitted changes. "+
		"If it fails: ssh pablo@gorgon.local, journalctl --user-unit mse-gorgon-update.service --since today, then systemctl --user start mse-gorgon-update.service to retry.", bodies[2]["desc"])
}

func TestSetUpDescribesTheDestination(t *testing.T) {
	for repository, destination := range map[string]string{"/mnt/backups": "a local folder", "s3:host/bucket": "s3 storage", "": "its backup repository"} {
		t.Run(destination, func(t *testing.T) {
			assert.Contains(t, description("backup", Facts{Name: "gorgon", Repository: repository}), "to "+destination+" with restic")
		})
	}
}

func TestSetUpWithoutATimeZoneUsesUTC(t *testing.T) {
	assert.Equal(t, "Etc/UTC", payload(Check{"update", "gorgon-update"}, Facts{Name: "gorgon"})["tz"])
}

func TestSetUpUnreachable(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("no route to host") })}

	set, failures := Manage{Client: client, URL: ChecksURL, Key: "k"}.SetUp(context.Background(), ChecksFor("gorgon", "secondary", "pi2"), Facts{Name: "gorgon"})

	assert.Empty(t, set)
	assert.Equal(t, []string{"gorgon-update-pi2: no route to host"}, failures)
}
