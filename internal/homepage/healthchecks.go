package homepage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const ChecksURL = "https://healthchecks.io/api/v3/checks/"

type CheckLister interface {
	Slugs(ctx context.Context, key string) (map[string]bool, error)
}

type Healthchecks struct {
	Client *http.Client
	URL    string
}

func (h Healthchecks) Slugs(ctx context.Context, key string) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("X-Api-Key", key)
	response, err := h.Client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("healthchecks answered %s", response.Status)
	}
	var listed struct {
		Checks []struct {
			Slug string `json:"slug"`
		} `json:"checks"`
	}
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		return nil, err
	}
	slugs := map[string]bool{}
	for _, check := range listed.Checks {
		slugs[check.Slug] = true
	}
	return slugs, nil
}
