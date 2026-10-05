package healthchecks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/pablovarela/media-server-engine/internal/paint"
)

const PingURL = "https://hc-ping.com"

var retryAfter = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

func Slug(name, job, role, shortHost string) string {
	slug := name + "-" + job
	if job == "update" && role != "main" {
		slug += "-" + shortHost
	}
	return slug
}

type Pings struct {
	Client  *http.Client
	URL     string
	Key     string
	Slug    func(job string) string
	Sleep   func(time.Duration)
	ErrOut  io.Writer
	Timeout time.Duration
	warned  bool
}

func (p *Pings) Ping(ctx context.Context, job, suffix string) {
	if p.Key == "" {
		p.warnOnce(fmt.Sprintf("no healthchecks ping key configured; %s is not reported", p.Slug(job)))
		return
	}
	check := p.Slug(job) + suffix
	address := p.URL + "/" + url.PathEscape(p.Key) + "/" + check + "?create=1"
	for attempt := 0; ; attempt++ {
		retry, err := p.attempt(ctx, address)
		if err == nil {
			return
		}
		if !retry || attempt == len(retryAfter) || ctx.Err() != nil {
			_, _ = fmt.Fprintln(p.ErrOut, paint.Stderr.Warning(fmt.Sprintf("healthchecks: could not report %s: %v", check, err)))
			return
		}
		p.Sleep(retryAfter[attempt])
	}
}

func (p *Pings) warnOnce(message string) {
	if !p.warned {
		p.warned = true
		_, _ = fmt.Fprintln(p.ErrOut, paint.Stderr.Warning(message))
	}
}

func (p *Pings) attempt(ctx context.Context, address string) (bool, error) {
	timeout := p.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return false, err
	}
	response, err := p.Client.Do(request)
	if err != nil {
		return true, withoutAddress(err)
	}
	_ = response.Body.Close()
	if response.StatusCode < http.StatusBadRequest {
		return false, nil
	}
	retry := response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError
	return retry, fmt.Errorf("healthchecks.io answered %d", response.StatusCode)
}

func withoutAddress(err error) error {
	var failed *url.Error
	if errors.As(err, &failed) {
		return failed.Err
	}
	return err
}
