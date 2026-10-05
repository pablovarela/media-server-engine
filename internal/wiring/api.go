package wiring

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

const (
	defaultAttempts = 5
	defaultRetry    = 3 * time.Second
	requestTimeout  = 60 * time.Second
	detailLimit     = 300
)

type API struct {
	Env      Env
	Base     string
	Headers  map[string]string
	Attempts int
	Retry    time.Duration
}

type attemptFailure struct {
	err   error
	again bool
}

func (a *API) Get(ctx context.Context, path string, into any) error {
	return a.do(ctx, http.MethodGet, path, nil, "", into)
}

func (a *API) Send(ctx context.Context, method, path string, body, into any) error {
	var payload []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = encoded
		a.Env.Redact.Sent(a.Headers, body)
	}
	return a.do(ctx, method, path, payload, "application/json", into)
}

func (a *API) SendForm(ctx context.Context, path string, form url.Values, into any) error {
	a.Env.Redact.Sent(a.Headers, form)
	return a.do(ctx, http.MethodPost, path, []byte(form.Encode()), "application/x-www-form-urlencoded", into)
}

func (a *API) do(ctx context.Context, method, path string, payload []byte, contentType string, into any) error {
	a.Env.Redact.Sent(a.Headers, nil)
	attempts := a.Attempts
	if attempts == 0 {
		attempts = defaultAttempts
	}
	retry := a.Retry
	if retry == 0 {
		retry = defaultRetry
	}
	for attempt := 1; ; attempt++ {
		answer, failure := a.once(ctx, method, path, payload, contentType)
		if failure == nil {
			return decode(answer, into)
		}
		if !failure.again || attempt >= attempts {
			return failure.err
		}
		if err := a.Env.Pause(ctx, retry); err != nil {
			return err
		}
	}
}

func decode(answer []byte, into any) error {
	if into == nil || len(bytes.TrimSpace(answer)) == 0 {
		return nil
	}
	return json.Unmarshal(answer, into)
}

func (a *API) once(ctx context.Context, method, path string, payload []byte, contentType string) ([]byte, *attemptFailure) {
	timed, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(timed, method, strings.TrimRight(a.Base, "/")+path, body)
	if err != nil {
		return nil, &attemptFailure{err: err}
	}
	for key, value := range a.Headers {
		request.Header.Set(key, value)
	}
	if payload != nil {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := a.Env.HTTP.Do(request)
	if err != nil {
		return nil, a.transportFailure(ctx, method, path, err)
	}
	defer func() { _ = response.Body.Close() }()
	answer, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, a.transportFailure(ctx, method, path, err)
	}
	if response.StatusCode < http.StatusBadRequest {
		return answer, nil
	}
	detail := a.Env.Redact.Hide(string(answer))
	if len(detail) > detailLimit {
		detail = detail[:detailLimit]
	}
	failure := Error{Message: fmt.Sprintf("%s %s answered %d: %s", method, path, response.StatusCode, detail)}
	return nil, &attemptFailure{err: failure, again: response.StatusCode == http.StatusServiceUnavailable}
}

func (a *API) transportFailure(ctx context.Context, method, path string, err error) *attemptFailure {
	if ctx.Err() != nil {
		return &attemptFailure{err: ctx.Err()}
	}
	failed := func(reason string) error {
		return Error{Message: fmt.Sprintf("%s %s failed: %s", method, path, reason)}
	}
	var timeout net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()):
		return &attemptFailure{err: failed("timed out")}
	case errors.Is(err, syscall.ECONNREFUSED):
		return &attemptFailure{err: failed("connection refused"), again: true}
	case errors.Is(err, syscall.ECONNRESET):
		return &attemptFailure{err: failed("connection reset"), again: method == http.MethodGet}
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
		return &attemptFailure{err: failed("connection closed before the answer"), again: method == http.MethodGet}
	}
	var wrapped *url.Error
	if errors.As(err, &wrapped) {
		err = wrapped.Err
	}
	return &attemptFailure{err: failed(err.Error())}
}

type Error struct {
	Message string
}

func (e Error) Error() string { return e.Message }
