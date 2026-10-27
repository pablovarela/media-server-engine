package downloads

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"github.com/pablovarela/media-server-engine/internal/installation"
)

const (
	startingRetry = 10 * time.Second
	startingTries = 30
)

func SearchMissing(ctx context.Context, client *http.Client, i *installation.Installation, out io.Writer, pause func(context.Context, time.Duration) error) error {
	var failures []error
	for _, app := range apps {
		q := queue{client: client, base: fmt.Sprintf("http://localhost:%d/api/v3/command", app.port), title: app.title, key: keyIn(filepath.Join(i.Data, app.config))}
		if q.key == "" {
			_, _ = fmt.Fprintf(out, "%s: not set up, skipped\n", app.name)
			continue
		}
		if err := q.startOnceAnswering(ctx, app.search, pause); err != nil {
			failures = append(failures, fmt.Errorf("%s: could not start the search: %w", app.name, err))
			continue
		}
		_, _ = fmt.Fprintf(out, "%s: searching for missing %s\n", app.name, app.missing)
	}
	return errors.Join(failures...)
}

func (q queue) startOnceAnswering(ctx context.Context, command map[string]any, pause func(context.Context, time.Duration) error) error {
	for try := 1; ; try++ {
		err := q.call(ctx, http.MethodPost, q.base, command, nil)
		if err == nil || !stillStarting(err) || try == startingTries+1 {
			return err
		}
		if err := pause(ctx, startingRetry); err != nil {
			return err
		}
	}
}

func stillStarting(err error) bool {
	var answered *answeredError
	return !errors.As(err, &answered) || answered.status >= http.StatusInternalServerError
}
