package downloads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pablovarela/media-server-engine/internal/installation"
)

const flag = "Found executable file"

var (
	ErrNotAllRemoved = errors.New("some flagged downloads could not be removed")
	apiKey           = regexp.MustCompile(`<ApiKey>(.*)</ApiKey>`)
	apps             = []struct {
		name, title string
		port        int
		config      string
	}{
		{"sonarr", "Sonarr", 8989, "volumes/sonarr/data/config.xml"},
		{"radarr", "Radarr", 7878, "volumes/radarr/config/config.xml"},
	}
)

type record struct {
	ID             int    `json:"id"`
	DownloadID     string `json:"downloadId"`
	Title          string `json:"title"`
	StatusMessages []struct {
		Messages []string `json:"messages"`
	} `json:"statusMessages"`
}

func Clean(ctx context.Context, client *http.Client, i *installation.Installation, pageSize int, out, errOut io.Writer) error {
	allRemoved := true
	for _, app := range apps {
		q := queue{client: client, base: fmt.Sprintf("http://localhost:%d/api/v3/queue", app.port), title: app.title, key: keyIn(filepath.Join(i.Data, app.config))}
		found, err := q.flagged(ctx, pageSize)
		if q.key == "" || err != nil {
			_, _ = fmt.Fprintf(out, "%s: queue not reachable, skipped\n", app.name)
			continue
		}
		removed := map[string]bool{}
		for _, r := range found {
			id := r.DownloadID
			if id == "" {
				id = fmt.Sprint(r.ID)
			}
			if removed[id] {
				continue
			}
			removed[id] = true
			if err := q.remove(ctx, r.ID); err != nil {
				_, _ = fmt.Fprintf(errOut, "%s: could not remove %s: %v\n", app.name, r.Title, err)
				allRemoved = false
				continue
			}
			_, _ = fmt.Fprintf(out, "%s: removed and blocklisted %s\n", app.name, r.Title)
		}
	}
	if !allRemoved {
		return ErrNotAllRemoved
	}
	return nil
}

func keyIn(path string) string {
	text, err := os.ReadFile(path) //nolint:gosec // reads the app's own config in the installation's data
	if err != nil {
		return ""
	}
	if found := apiKey.FindSubmatch(text); found != nil {
		return string(found[1])
	}
	return ""
}

type queue struct {
	client *http.Client
	base   string
	title  string
	key    string
}

func (q queue) flagged(ctx context.Context, pageSize int) ([]record, error) {
	if q.key == "" {
		return nil, nil
	}
	var found []record
	for page := 1; ; page++ {
		var listed struct {
			TotalRecords int      `json:"totalRecords"`
			Records      []record `json:"records"`
		}
		if err := q.call(ctx, http.MethodGet, fmt.Sprintf("%s?page=%d&pageSize=%d", q.base, page, pageSize), &listed); err != nil {
			return nil, err
		}
		for _, r := range listed.Records {
			if isFlagged(r) {
				found = append(found, r)
			}
		}
		if page*pageSize >= listed.TotalRecords {
			return found, nil
		}
	}
}

func isFlagged(r record) bool {
	for _, status := range r.StatusMessages {
		for _, message := range status.Messages {
			if strings.Contains(message, flag) {
				return true
			}
		}
	}
	return false
}

func (q queue) remove(ctx context.Context, id int) error {
	return q.call(ctx, http.MethodDelete, fmt.Sprintf("%s/%d?removeFromClient=true&blocklist=true&skipRedownload=false", q.base, id), nil)
}

func (q queue) call(ctx context.Context, method, url string, into any) error {
	request, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("X-Api-Key", q.key)
	response, err := q.client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("%s answered %d", q.title, response.StatusCode)
	}
	if into == nil {
		return nil
	}
	return json.NewDecoder(response.Body).Decode(into)
}
