package downloads

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"

	"github.com/pablovarela/media-server-engine/internal/installation"
)

var missingSearches = map[string]struct {
	command map[string]any
	what    string
}{
	"sonarr": {command: map[string]any{"name": "MissingEpisodeSearch", "monitored": true}, what: "episodes"},
	"radarr": {command: map[string]any{"name": "MissingMoviesSearch"}, what: "movies"},
}

func SearchMissing(ctx context.Context, client *http.Client, i *installation.Installation, out io.Writer) error {
	var failures []error
	for _, app := range apps {
		key := keyIn(filepath.Join(i.Data, app.config))
		if key == "" {
			_, _ = fmt.Fprintf(out, "%s: not set up, skipped\n", app.name)
			continue
		}
		search := missingSearches[app.name]
		if err := startCommand(ctx, client, app.port, app.title, key, search.command); err != nil {
			failures = append(failures, fmt.Errorf("%s: could not start the search: %w", app.name, err))
			continue
		}
		_, _ = fmt.Fprintf(out, "%s: searching for missing %s\n", app.name, search.what)
	}
	return errors.Join(failures...)
}

func startCommand(ctx context.Context, client *http.Client, port int, title, key string, command map[string]any) error {
	body, err := json.Marshal(command)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://localhost:%d/api/v3/command", port), bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("X-Api-Key", key)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		var failed *url.Error
		if errors.As(err, &failed) {
			return failed.Err
		}
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("%s answered %d %s", title, response.StatusCode, http.StatusText(response.StatusCode))
	}
	return nil
}
