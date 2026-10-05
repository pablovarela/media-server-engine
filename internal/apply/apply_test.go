package apply

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/report"
)

var errRegistryLimit = errors.New("toomanyrequests: You have reached your pull rate limit")

type Given struct {
	pulls    []error
	up       error
	reattach error
	wire     error
	wiring   bool
}

type Then struct {
	err      string
	stdout   string
	reattach bool
	wired    bool
	reloaded bool
	pruned   bool
	slept    []time.Duration
}

func TestRun(t *testing.T) {
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"everything works": {
			Given: Given{pulls: []error{nil}, wiring: true},
			Then: Then{
				reattach: true, wired: true, reloaded: true, pruned: true,
				stdout: "Setting up the Healthchecks checks... gorgon-update.\nCreating the data folders... done.\nPulling images... pulled 2 images.\n" +
					"Starting the stack... recreated 2 containers.\nReattaching to gluetun... nothing to reattach.\nWiring the apps... nothing to change.\n",
			},
		},
		"without wiring there is no wiring step": {
			Given: Given{pulls: []error{nil}},
			Then: Then{
				reattach: true, reloaded: true, pruned: true,
				stdout: "Setting up the Healthchecks checks... gorgon-update.\nCreating the data folders... done.\nPulling images... pulled 2 images.\n" +
					"Starting the stack... recreated 2 containers.\nReattaching to gluetun... nothing to reattach.\n",
			},
		},
		"rate limited, then pulled": {
			Given: Given{pulls: []error{errRegistryLimit, errRegistryLimit, nil}},
			Then:  Then{reattach: true, reloaded: true, pruned: true, slept: []time.Duration{30 * time.Second, 60 * time.Second}},
		},
		"rate limited every time restarts nothing": {
			Given: Given{pulls: []error{errRegistryLimit, errRegistryLimit, errRegistryLimit, errRegistryLimit}},
			Then: Then{
				err:   "a registry kept refusing pulls as too many requests; try again later",
				slept: []time.Duration{30 * time.Second, 60 * time.Second, 90 * time.Second},
			},
		},
		"another pull failure restarts nothing": {
			Given: Given{pulls: []error{errors.New("manifest unknown")}},
			Then:  Then{err: "manifest unknown"},
		},
		"up fails: still reattaches, then stops before the wiring": {
			Given: Given{pulls: []error{nil}, up: errors.New("deluge: port 8112 in use"), wiring: true},
			Then:  Then{err: "deluge: port 8112 in use", reattach: true},
		},
		"reattaching fails: stops before the wiring": {
			Given: Given{pulls: []error{nil}, reattach: errors.New("no such container"), wiring: true},
			Then:  Then{err: "no such container", reattach: true},
		},
		"wiring fails: still reloads the page, skips the prune": {
			Given: Given{pulls: []error{nil}, wire: errors.New("wiring failed: seerr"), wiring: true},
			Then:  Then{err: "wiring failed: seerr", reattach: true, wired: true, reloaded: true},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			stack, checks, page, images, wiring := expectedRun(t, ctx, tt.Given, tt.Then)
			var made []string
			var slept []time.Duration
			var stdout, stderr bytes.Buffer
			a := &Apply{
				Stack: stack, Checks: checks, Wiring: wiring, Page: page, Images: images,
				MkdirAll: func(path string) error { made = append(made, path); return nil },
				Sleep:    func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil },
				Report:   report.New(&stdout, &stderr, nil),
			}

			err := a.Run(ctx)

			if tt.Then.err != "" {
				assert.EqualError(t, err, tt.Then.err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, []string{"/data/volumes/jellyfin"}, made)
			assert.Equal(t, tt.Then.slept, slept)
			if tt.Then.stdout != "" {
				assert.Equal(t, tt.Then.stdout, stdout.String())
			}
		})
	}
}

func expectedRun(t *testing.T, ctx context.Context, given Given, then Then) (*mockStack, *mockChecks, *mockPage, *mockImages, Wiring) {
	t.Helper()
	stack := newMockStack(t)
	stack.EXPECT().BindSources().Return([]string{"/data/volumes/jellyfin"})
	for _, err := range given.pulls {
		stack.EXPECT().Pull(ctx).Return("pulled 2 images", err).Once()
	}
	if given.pulls[len(given.pulls)-1] == nil {
		stack.EXPECT().Up(ctx).Return("recreated 2 containers", given.up)
	}
	if then.reattach {
		stack.EXPECT().Reattach(ctx).Return("nothing to reattach", given.reattach)
	}
	checks := newMockChecks(t)
	checks.EXPECT().SetUp(ctx).Return([]string{"gorgon-update"}, nil)
	page, images := newMockPage(t), newMockImages(t)
	if then.reloaded {
		page.EXPECT().Reload(ctx).Return(nil)
	}
	if then.pruned {
		images.EXPECT().Prune(ctx).Return(nil)
	}
	if !given.wiring {
		return stack, checks, page, images, nil
	}
	w := newMockWiring(t)
	if then.wired {
		w.EXPECT().Wire(ctx).Return("nothing to change", given.wire)
	}
	return stack, checks, page, images, w
}

func TestRunWarnsWhenAChecksSetUpFails(t *testing.T) {
	ctx := context.Background()
	stack := newMockStack(t)
	stack.EXPECT().BindSources().Return(nil)
	stack.EXPECT().Pull(ctx).Return("pulled 0 images", nil)
	stack.EXPECT().Up(ctx).Return("up to date", nil)
	stack.EXPECT().Reattach(ctx).Return("nothing to reattach", nil)
	checks := newMockChecks(t)
	checks.EXPECT().SetUp(ctx).Return(nil, []string{"gorgon-backup: healthchecks.io answered 401"})
	page, images := newMockPage(t), newMockImages(t)
	page.EXPECT().Reload(ctx).Return(nil)
	images.EXPECT().Prune(ctx).Return(nil)
	var stdout, stderr bytes.Buffer

	err := (&Apply{
		Stack: stack, Checks: checks, Page: page, Images: images,
		MkdirAll: func(string) error { return nil }, Sleep: func(context.Context, time.Duration) error { return nil }, Report: report.New(&stdout, &stderr, nil),
	}).Run(ctx)

	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Setting up the Healthchecks checks... none set up.\n")
	assert.Contains(t, stderr.String(), "healthchecks: could not set up gorgon-backup: healthchecks.io answered 401")
}

func TestRunWithoutChecksSkipsTheirStep(t *testing.T) {
	ctx := context.Background()
	stack := newMockStack(t)
	stack.EXPECT().BindSources().Return(nil)
	stack.EXPECT().Pull(ctx).Return("pulled 0 images", nil)
	stack.EXPECT().Up(ctx).Return("up to date", nil)
	stack.EXPECT().Reattach(ctx).Return("nothing to reattach", nil)
	page, images := newMockPage(t), newMockImages(t)
	page.EXPECT().Reload(ctx).Return(nil)
	images.EXPECT().Prune(ctx).Return(nil)
	var stdout bytes.Buffer

	err := (&Apply{
		Stack: stack, Page: page, Images: images,
		MkdirAll: func(string) error { return nil }, Sleep: func(context.Context, time.Duration) error { return nil }, Report: report.New(&stdout, &stdout, nil),
	}).Run(ctx)

	require.NoError(t, err)
	assert.NotContains(t, stdout.String(), "Healthchecks")
}

func TestRunStopsWhenADataFolderCannotBeCreated(t *testing.T) {
	stack := newMockStack(t)
	stack.EXPECT().BindSources().Return([]string{"/data/volumes/jellyfin"})
	var stdout bytes.Buffer

	err := (&Apply{
		Stack: stack, MkdirAll: func(string) error { return errors.New("permission denied") },
		Sleep: func(context.Context, time.Duration) error { return nil }, Report: report.New(&stdout, &stdout, nil),
	}).Run(context.Background())

	assert.EqualError(t, err, "permission denied")
}

func TestRunStopsRetryingThePullWhenInterrupted(t *testing.T) {
	ctx := context.Background()
	stack := newMockStack(t)
	stack.EXPECT().BindSources().Return(nil)
	stack.EXPECT().Pull(ctx).Return("", errRegistryLimit).Once()
	var stdout bytes.Buffer

	err := (&Apply{
		Stack: stack, MkdirAll: func(string) error { return nil }, Report: report.New(&stdout, &stdout, nil),
		Sleep: func(context.Context, time.Duration) error { return context.Canceled },
	}).Run(ctx)

	assert.ErrorIs(t, err, context.Canceled)
}

func TestAFailedWiringDoesNotPrintAnotherToolsLinesAsItsCause(t *testing.T) {
	ctx := context.Background()
	stack := newMockStack(t)
	stack.EXPECT().BindSources().Return(nil)
	stack.EXPECT().Pull(ctx).Return("pulled 0 images", nil)
	stack.EXPECT().Up(ctx).Return("up to date", nil)
	stack.EXPECT().Reattach(ctx).Return("nothing to reattach", nil)
	page := newMockPage(t)
	page.EXPECT().Reload(ctx).Return(nil)
	var stdout, stderr bytes.Buffer
	r := report.New(&stdout, &stderr, nil)
	wiring := newMockWiring(t)
	wiring.EXPECT().Wire(ctx).RunAndReturn(func(context.Context) (string, error) {
		_, _ = r.Tool("configarr").Write([]byte("INFO configarr finished\n"))
		return "", errors.New("wiring failed: seerr")
	})

	err := (&Apply{
		Stack: stack, Wiring: wiring, Page: page, MkdirAll: func(string) error { return nil },
		Sleep: func(context.Context, time.Duration) error { return nil }, Report: r,
	}).Run(ctx)

	assert.EqualError(t, err, "wiring failed: seerr")
	assert.NotContains(t, stderr.String(), "configarr finished")
}

func succeeding(t *testing.T, ctx context.Context) (*mockStack, *mockPage, *mockImages) {
	t.Helper()
	stack := newMockStack(t)
	stack.EXPECT().BindSources().Return(nil)
	stack.EXPECT().Pull(ctx).Return("13 up to date", nil)
	stack.EXPECT().Up(ctx).Return("up to date", nil)
	stack.EXPECT().Reattach(ctx).Return("nothing to reattach", nil)
	page, images := newMockPage(t), newMockImages(t)
	page.EXPECT().Reload(ctx).Return(nil)
	images.EXPECT().Prune(ctx).Return(nil)
	return stack, page, images
}

func TestTheTimersAreSetUpLast(t *testing.T) {
	ctx := context.Background()
	stack, page, images := succeeding(t, ctx)
	timers := newMockTimers(t)
	timers.EXPECT().Set(ctx).Return("all 4 unchanged", nil, nil)
	var stdout bytes.Buffer

	err := (&Apply{
		Stack: stack, Page: page, Images: images, Timers: timers, MkdirAll: func(string) error { return nil },
		Sleep: func(context.Context, time.Duration) error { return nil }, Report: report.New(&stdout, &stdout, nil),
	}).Run(ctx)

	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(stdout.String(), "Setting up the timers... all 4 unchanged.\n"), stdout.String())
}

func TestAFailedTimersStepFailsTheApply(t *testing.T) {
	ctx := context.Background()
	stack, page, images := succeeding(t, ctx)
	timers := newMockTimers(t)
	timers.EXPECT().Set(ctx).Return("", nil, errors.New("systemctl --user daemon-reload failed: Failed to connect to bus"))
	var stdout bytes.Buffer

	err := (&Apply{
		Stack: stack, Page: page, Images: images, Timers: timers, MkdirAll: func(string) error { return nil },
		Sleep: func(context.Context, time.Duration) error { return nil }, Report: report.New(&stdout, &stdout, nil),
	}).Run(ctx)

	assert.EqualError(t, err, "systemctl --user daemon-reload failed: Failed to connect to bus")
}

func TestTheTimersAreSetUpEvenWhenAnEarlierStepFails(t *testing.T) {
	ctx := context.Background()
	stack := newMockStack(t)
	stack.EXPECT().BindSources().Return(nil)
	stack.EXPECT().Pull(ctx).Return("", errors.New("manifest unknown"))
	timers := newMockTimers(t)
	timers.EXPECT().Set(ctx).Return("all 4 unchanged", nil, nil)
	var stdout bytes.Buffer

	err := (&Apply{
		Stack: stack, Timers: timers, MkdirAll: func(string) error { return nil },
		Sleep: func(context.Context, time.Duration) error { return nil }, Report: report.New(&stdout, &stdout, nil),
	}).Run(ctx)

	assert.EqualError(t, err, "manifest unknown")
	assert.Contains(t, stdout.String(), "Setting up the timers... all 4 unchanged.")
}

func TestAStoppedApplySetsUpNoTimers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stack := newMockStack(t)
	stack.EXPECT().BindSources().Return(nil)
	stack.EXPECT().Pull(mock.Anything).RunAndReturn(func(context.Context) (string, error) {
		cancel()
		return "", context.Canceled
	})
	var stdout bytes.Buffer

	err := (&Apply{
		Stack: stack, Timers: newMockTimers(t), MkdirAll: func(string) error { return nil },
		Sleep: func(context.Context, time.Duration) error { return nil }, Report: report.New(&stdout, &stdout, nil),
	}).Run(ctx)

	assert.ErrorIs(t, err, context.Canceled)
}

func TestTheTimersWarningsFollowTheirResult(t *testing.T) {
	ctx := context.Background()
	stack, page, images := succeeding(t, ctx)
	timers := newMockTimers(t)
	timers.EXPECT().Set(ctx).Return("all 4 unchanged", []string{"the nightly update can't get a GitHub token"}, nil)
	var out bytes.Buffer

	err := (&Apply{
		Stack: stack, Page: page, Images: images, Timers: timers, MkdirAll: func(string) error { return nil },
		Sleep: func(context.Context, time.Duration) error { return nil }, Report: report.New(&out, &out, nil),
	}).Run(ctx)

	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(out.String(), "Setting up the timers... all 4 unchanged.\nthe nightly update can't get a GitHub token\n"), out.String())
}
