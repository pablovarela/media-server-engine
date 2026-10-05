package wiring

import (
	"context"
	"io"
	"net/url"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func appAPI(env Env) *API {
	return &API{Env: env, Base: "http://app/", Headers: map[string]string{"X-Api-Key": "header-key"}}
}

func TestARequestIsSentToTheAppsAddressWithItsHeadersAndBody(t *testing.T) {
	env, _ := testEnv(t, exchanges(t, exchange{
		method: "POST", url: "http://app/api/thing", body: `{"name":"x"}`,
		header: map[string]string{"X-Api-Key": "header-key", "Content-Type": "application/json"}, answer: `{"id":1}`,
	}), nil)
	var answer map[string]any

	require.NoError(t, appAPI(env).Send(context.Background(), "POST", "/api/thing", map[string]any{"name": "x"}, &answer))

	assert.Equal(t, map[string]any{"id": float64(1)}, answer)
}

func TestAFormIsSentURLEncoded(t *testing.T) {
	env, _ := testEnv(t, exchanges(t, exchange{
		method: "POST", url: "http://app/api/settings", body: "languages=en",
		header: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
	}), nil)

	require.NoError(t, appAPI(env).SendForm(context.Background(), "/api/settings", url.Values{"languages": {"en"}}, nil))
}

func TestAnAppThatResetsAndThenAnswers503WhileStartingIsAskedAgainUntilItAnswers(t *testing.T) {
	env, _ := testEnv(t, exchanges(t,
		exchange{method: "GET", url: "http://app/System/Info", err: syscall.ECONNRESET},
		exchange{method: "GET", url: "http://app/System/Info", status: 503, answer: "starting"},
		exchange{method: "GET", url: "http://app/System/Info", answer: `{"ServerName":"Media"}`},
	), nil)
	var answer map[string]any

	require.NoError(t, appAPI(env).Get(context.Background(), "/System/Info", &answer))

	assert.Equal(t, "Media", answer["ServerName"])
}

func TestAnAppThatKeepsAnswering503FailsAfterTheAttemptsRunOut(t *testing.T) {
	starting := exchange{method: "GET", url: "http://app/System/Info", status: 503, answer: "starting"}
	env, _ := testEnv(t, exchanges(t, starting, starting, starting), nil)
	api := appAPI(env)
	api.Attempts = 3

	err := api.Get(context.Background(), "/System/Info", nil)

	assert.EqualError(t, err, "GET /System/Info answered 503: starting")
}

func TestAWriteTheAppCannotHaveProcessedIsSentAgain(t *testing.T) {
	for name, first := range map[string]exchange{
		"503":                {method: "POST", url: "http://app/System/Configuration", body: "{}", status: 503, answer: "starting"},
		"connection refused": {method: "POST", url: "http://app/System/Configuration", body: "{}", err: syscall.ECONNREFUSED},
	} {
		t.Run(name, func(t *testing.T) {
			env, _ := testEnv(t, exchanges(t, first, exchange{method: "POST", url: "http://app/System/Configuration", body: "{}"}), nil)

			require.NoError(t, appAPI(env).Send(context.Background(), "POST", "/System/Configuration", map[string]any{}, nil))
		})
	}
}

func TestAWriteWhoseConnectionIsResetIsNotSentAgainSinceTheAppMayHaveActedOnIt(t *testing.T) {
	for name, reset := range map[string]error{
		"reset":                    &url.Error{Op: "Post", URL: "http://app", Err: syscall.ECONNRESET},
		"closed before the answer": &url.Error{Op: "Post", URL: "http://app", Err: io.ErrUnexpectedEOF},
	} {
		t.Run(name, func(t *testing.T) {
			env, _ := testEnv(t, exchanges(t, exchange{method: "POST", url: "http://app/System/Configuration", body: "{}", err: reset}), nil)

			err := appAPI(env).Send(context.Background(), "POST", "/System/Configuration", map[string]any{}, nil)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "POST /System/Configuration failed")
		})
	}
}

func TestFewerThanOneAttemptStillSendsTheRequestOnce(t *testing.T) {
	env, _ := testEnv(t, exchanges(t, exchange{method: "POST", url: "http://app/System/Configuration", body: "{}"}), nil)
	api := appAPI(env)
	api.Attempts = -1

	require.NoError(t, api.Send(context.Background(), "POST", "/System/Configuration", map[string]any{}, nil))
}

func TestAnAppThatRejectsARequestFailsAtOnceWithItsReason(t *testing.T) {
	env, _ := testEnv(t, exchanges(t, exchange{
		method: "PUT", url: "http://app/api/v1/settings/jellyfin", body: `{"name":"x"}`, status: 400, answer: `{"message": "request/body/name is read-only"}`,
	}), nil)

	err := appAPI(env).Send(context.Background(), "PUT", "/api/v1/settings/jellyfin", map[string]any{"name": "x"}, nil)

	assert.EqualError(t, err, `PUT /api/v1/settings/jellyfin answered 400: {"message": "request/body/name is read-only"}`)
}

func TestAnAppsErrorThatEchoesASecretBackIsReportedWithTheSecretHidden(t *testing.T) {
	env, _ := testEnv(t, exchanges(t, exchange{
		method: "POST", url: "http://app/api/connect", body: `{"apiKey":"body-token"}`, status: 400,
		answer: "sonarr at sonarr-key refused, sent with header-key and body-token",
	}), map[string]string{"SONARR_API_KEY": "sonarr-key"})

	err := appAPI(env).Send(context.Background(), "POST", "/api/connect", map[string]any{"apiKey": "body-token"}, nil)

	require.Error(t, err)
	assert.Equal(t, "POST /api/connect answered 400: sonarr at <hidden> refused, sent with <hidden> and <hidden>", env.Redact.Hide(err.Error()))
}

func TestARequestThatTimesOutFailsAtOnceWithoutWaitingForMoreAttempts(t *testing.T) {
	env, _ := testEnv(t, exchanges(t, exchange{method: "GET", url: "http://app/System/Info", err: context.DeadlineExceeded}), nil)

	err := appAPI(env).Get(context.Background(), "/System/Info", nil)

	assert.EqualError(t, err, "GET /System/Info failed: timed out")
}

func TestAnEmptyAnswerDecodesToNothing(t *testing.T) {
	env, _ := testEnv(t, exchanges(t, exchange{method: "POST", url: "http://app/Startup/Complete"}), nil)
	var answer map[string]any

	require.NoError(t, appAPI(env).Send(context.Background(), "POST", "/Startup/Complete", nil, &answer))

	assert.Nil(t, answer)
}

func TestAWaitBetweenAttemptsEndsWhenCancelled(t *testing.T) {
	env, _ := testEnv(t, exchanges(t, exchange{method: "GET", url: "http://app/System/Info", status: 503}), nil)
	env.Pause = func(context.Context, time.Duration) error { return context.Canceled }

	err := appAPI(env).Get(context.Background(), "/System/Info", nil)

	assert.ErrorIs(t, err, context.Canceled)
}
