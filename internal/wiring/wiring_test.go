package wiring

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testHealth = map[string]Health{
	"prowlarr": {Variable: "PROWLARR_URL", Fallback: "http://localhost:9696", Path: "/ping"},
	"jellyfin": {Variable: "JELLYFIN_URL", Fallback: "http://localhost:8096", Path: "/System/Info/Public"},
	"sonarr":   {Variable: "SONARR_URL", Fallback: "http://localhost:8989", Path: "/ping"},
	"radarr":   {Variable: "RADARR_URL", Fallback: "http://localhost:7878", Path: "/ping"},
	"seerr":    {Variable: "SEERR_URL", Fallback: "http://localhost:5055", Path: "/api/v1/status"},
}

type run struct {
	ran      []string
	warnings []string
}

func (r *run) step(name string, apps []string, err error) Step {
	return Step{Name: name, Apps: apps, Run: func(context.Context, Env) error {
		r.ran = append(r.ran, name)
		return err
	}}
}

func wiringFor(env Env, r *run, steps ...Step) *Wiring {
	now := time.Date(2026, 10, 6, 5, 0, 0, 0, time.UTC)
	return &Wiring{
		Env: env, Steps: steps, Health: testHealth, Now: func() time.Time { return now },
		Warn: func(line string) { r.warnings = append(r.warnings, line) },
	}
}

var prowlarrPing = exchange{method: "GET", url: "http://localhost:9696/ping"}

func TestWiringRunsEveryStepInOrder(t *testing.T) {
	env, _ := testEnv(t, exchanges(t, prowlarrPing, prowlarrPing, prowlarrPing), nil)
	r := &run{}

	result, err := wiringFor(env, r, r.step("first", []string{"prowlarr"}, nil), r.step("second", []string{"prowlarr"}, nil), r.step("third", []string{"prowlarr"}, nil)).Wire(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "nothing to change", result)
	assert.Equal(t, []string{"first", "second", "third"}, r.ran)
}

func TestAFailingStepDoesNotStopTheOthersButFailsTheRunAndIsNamed(t *testing.T) {
	env, _ := testEnv(t, exchanges(t), nil)
	r := &run{}

	_, err := wiringFor(env, r, r.step("first", nil, nil), r.step("second", nil, errors.New("no library Cartoons")), r.step("third", nil, nil)).Wire(context.Background())

	assert.EqualError(t, err, "wiring failed: second")
	assert.Equal(t, []string{"first", "second", "third"}, r.ran)
	assert.Equal(t, []string{"second: no library Cartoons"}, r.warnings)
}

func TestAStepsErrorIsReportedWithTheSecretsHidden(t *testing.T) {
	env, _ := testEnv(t, exchanges(t), map[string]string{"SONARR_API_KEY": "sonarr-key"})
	r := &run{}

	_, err := wiringFor(env, r, r.step("seerr", nil, errors.New("sonarr refused sonarr-key"))).Wire(context.Background())

	require.Error(t, err)
	assert.Equal(t, []string{"seerr: sonarr refused <hidden>"}, r.warnings)
}

func TestWiringWithNoStepsSucceeds(t *testing.T) {
	env, _ := testEnv(t, exchanges(t), nil)

	result, err := wiringFor(env, &run{}).Wire(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "nothing to change", result)
}

func TestTheResultCountsTheChanges(t *testing.T) {
	for changes, want := range map[int]string{1: "1 change", 3: "3 changes"} {
		t.Run(want, func(t *testing.T) {
			env, out := testEnv(t, exchanges(t), nil)
			step := Step{Name: "prowlarr", Run: func(_ context.Context, env Env) error {
				for n := range changes {
					env.Change("prowlarr", fmt.Sprintf("add tag %d", n))
				}
				return nil
			}}

			result, err := wiringFor(env, &run{}, step).Wire(context.Background())

			require.NoError(t, err)
			assert.Equal(t, want, result)
			assert.Equal(t, "prowlarr: add tag 0", out.lines[0])
		})
	}
}

func TestAStepWaitsUntilItsAppAnswers(t *testing.T) {
	env, _ := testEnv(t, exchanges(t,
		exchange{method: "GET", url: "http://localhost:9696/ping", err: syscall.ECONNREFUSED},
		exchange{method: "GET", url: "http://localhost:9696/ping", status: 503, answer: "starting"},
		exchange{method: "GET", url: "http://localhost:9696/ping", status: 401},
		prowlarrPing,
	), nil)
	var paused []time.Duration
	env.Pause = func(_ context.Context, d time.Duration) error { paused = append(paused, d); return nil }
	r := &run{}

	_, err := wiringFor(env, r, r.step("prowlarr", []string{"prowlarr"}, nil)).Wire(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"prowlarr"}, r.ran)
	assert.Equal(t, []time.Duration{2 * time.Second, 2 * time.Second, 2 * time.Second}, paused)
}

func TestAnAppThatNeverAnswersIsSkippedAndNamedAndTheRestStillRun(t *testing.T) {
	env, _ := testEnv(t, exchanges(t,
		exchange{method: "GET", url: "http://localhost:9696/ping", err: syscall.ECONNREFUSED},
		exchange{method: "GET", url: "http://localhost:9696/ping", err: syscall.ECONNREFUSED},
		exchange{method: "GET", url: "http://localhost:8096/System/Info/Public"},
	), nil)
	now := time.Date(2026, 10, 6, 5, 0, 0, 0, time.UTC)
	env.Pause = func(_ context.Context, d time.Duration) error { now = now.Add(d); return nil }
	r := &run{}
	w := wiringFor(env, r, r.step("prowlarr", []string{"prowlarr"}, nil), r.step("jellyfin", []string{"jellyfin"}, nil))
	w.Now = func() time.Time { return now }
	w.Wait = time.Second

	_, err := w.Wire(context.Background())

	assert.EqualError(t, err, "wiring failed: prowlarr")
	assert.Equal(t, []string{"jellyfin"}, r.ran)
	assert.Equal(t, []string{"prowlarr: prowlarr not answering after 1s; skipped its wiring"}, r.warnings)
}

func TestOneDeadlineForTheWholeRun(t *testing.T) {
	refused := exchange{method: "GET", url: "http://localhost:9696/ping", err: syscall.ECONNREFUSED}
	jellyfinRefused := exchange{method: "GET", url: "http://localhost:8096/System/Info/Public", err: syscall.ECONNREFUSED}
	env, _ := testEnv(t, exchanges(t, refused, refused, prowlarrPing, jellyfinRefused, jellyfinRefused), nil)
	now := time.Date(2026, 10, 6, 5, 0, 0, 0, time.UTC)
	env.Pause = func(_ context.Context, d time.Duration) error { now = now.Add(2 * time.Minute); return nil }
	r := &run{}
	w := wiringFor(env, r, r.step("prowlarr", []string{"prowlarr"}, nil), r.step("jellyfin", []string{"jellyfin"}, nil))
	w.Now = func() time.Time { return now }

	_, err := w.Wire(context.Background())

	assert.EqualError(t, err, "wiring failed: jellyfin")
	assert.Equal(t, []string{"prowlarr"}, r.ran)
	assert.Equal(t, []string{"jellyfin: jellyfin not answering after 300s; skipped its wiring"}, r.warnings)
}

func TestStepsWithoutAnAppOfTheirOwnDoNotWait(t *testing.T) {
	env, _ := testEnv(t, exchanges(t), nil)
	r := &run{}

	_, err := wiringFor(env, r, r.step("configarr", nil, nil)).Wire(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"configarr"}, r.ran)
}

func TestAStepThatChangesTwoAppsWaitsForBoth(t *testing.T) {
	env, _ := testEnv(t, exchanges(t,
		exchange{method: "GET", url: "http://localhost:8989/ping"},
		exchange{method: "GET", url: "http://localhost:7878/ping"},
	), nil)
	r := &run{}

	_, err := wiringFor(env, r, r.step("library-updates", []string{"sonarr", "radarr"}, nil)).Wire(context.Background())

	require.NoError(t, err)
}

func TestAnAppsAddressCanBeSetForThisMachine(t *testing.T) {
	env, _ := testEnv(t, exchanges(t, exchange{method: "GET", url: "http://seerr.example:5055/api/v1/status"}), nil)
	env.Settings["SEERR_URL"] = "http://seerr.example:5055/"
	r := &run{}

	_, err := wiringFor(env, r, r.step("seerr", []string{"seerr"}, nil)).Wire(context.Background())

	require.NoError(t, err)
}

func TestAppsGetFiveMinutesToAnswerEnoughForADatabaseMigrationOnARaspberryPi(t *testing.T) {
	assert.Equal(t, 5*time.Minute, (&Wiring{}).wait())
}

func TestWireStopsWhenCancelled(t *testing.T) {
	env, _ := testEnv(t, exchanges(t), nil)
	ctx, cancel := context.WithCancel(context.Background())
	r := &run{}
	first := Step{Name: "first", Run: func(context.Context, Env) error { cancel(); return context.Canceled }}

	_, err := wiringFor(env, r, first, r.step("second", nil, nil)).Wire(ctx)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, r.ran)
	assert.Empty(t, r.warnings)
}

func TestAWaitForAnAppEndsWhenCancelled(t *testing.T) {
	env, _ := testEnv(t, exchanges(t, exchange{method: "GET", url: "http://localhost:9696/ping", err: syscall.ECONNREFUSED}), nil)
	env.Pause = func(context.Context, time.Duration) error { return context.Canceled }
	r := &run{}

	_, err := wiringFor(env, r, r.step("prowlarr", []string{"prowlarr"}, nil)).Wire(context.Background())

	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, r.ran)
}
