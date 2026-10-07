package create

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/pablovarela/media-server-engine/internal/configure"
	"github.com/pablovarela/media-server-engine/internal/report"
)

var gorgon = Installation{Name: "gorgon", Config: "/c/gorgon", Logs: "/s/gorgon/logs"}

type stepFailure struct {
	at      string
	err     error
	created bool
}

type runFixture struct {
	steps         *mockSteps
	order, undone []string
	out           bytes.Buffer
}

func failingRun(t *testing.T, failure stepFailure) (*runFixture, error) {
	f := &runFixture{steps: newMockSteps(t)}
	undo := func(name string) Undo { return func() error { f.undone = append(f.undone, name); return nil } }
	fails := func(name string) error {
		f.order = append(f.order, name)
		if name == failure.at {
			return failure.err
		}
		return nil
	}
	f.steps.EXPECT().CheckName(mock.Anything).RunAndReturn(func(context.Context) error { return fails("name") }).Maybe()
	f.steps.EXPECT().AddKey(mock.Anything).RunAndReturn(func(context.Context) (Undo, error) { return undo("key"), fails("key") }).Maybe()
	f.steps.EXPECT().WriteConfig(mock.Anything).RunAndReturn(func(context.Context) (Undo, error) { return undo("config"), fails("config") }).Maybe()
	f.steps.EXPECT().Configure(mock.Anything).RunAndReturn(func(context.Context) error { return fails("settings") }).Maybe()
	f.steps.EXPECT().Commit(mock.Anything).RunAndReturn(func(context.Context) error { return fails("commit") }).Maybe()
	f.steps.EXPECT().Publish(mock.Anything).RunAndReturn(func(context.Context) (bool, error) {
		err := fails("publish")
		return err == nil || failure.created, err
	}).Maybe()
	f.steps.EXPECT().ClaimMain(mock.Anything).RunAndReturn(func(context.Context) error { return fails("main") }).Maybe()
	f.steps.EXPECT().Apply(mock.Anything).RunAndReturn(func(context.Context) error { return fails("apply") }).Maybe()
	f.steps.EXPECT().Summary(mock.Anything).Return("gorgon is ready.").Maybe()
	err := Run(context.Background(), gorgon, f.steps, report.New(&f.out, &f.out, nil), func() func() { return func() {} })
	return f, err
}

func TestRunGoesThroughEveryStep(t *testing.T) {
	f, err := failingRun(t, stepFailure{})

	assert.NoError(t, err)
	assert.Equal(t, []string{"name", "key", "config", "settings", "commit", "publish", "main", "apply"}, f.order)
	assert.Empty(t, f.undone)
	assert.Contains(t, f.out.String(), "gorgon is ready.\n")
}

var errBroken = errors.New("broken")

func TestRunStopsBeforeWritingAnything(t *testing.T) {
	for _, at := range []string{"name"} {
		t.Run(at, func(t *testing.T) {
			f, err := failingRun(t, stepFailure{at: at, err: errBroken})

			assert.ErrorIs(t, err, errBroken)
			assert.Empty(t, f.undone)
			assert.Equal(t, at, f.order[len(f.order)-1])
		})
	}
}

func TestRunUndoesWhatItWrote(t *testing.T) {
	tests := map[string]struct {
		undone  []string
		message string
	}{
		"key": {[]string{"key"}, "broken\nNothing was kept; run mse setup gorgon again.\n" +
			"The secrets key shown for gorgon was removed; if you saved it, delete it from your password manager."},
		"config": {[]string{"config", "key"}, "broken\nNothing was kept apart from this run's log in /s/gorgon/logs; run mse setup gorgon again.\n" +
			"The secrets key shown for gorgon was removed; if you saved it, delete it from your password manager."},
		"settings": {[]string{"config", "key"}, "broken\nNothing was kept apart from this run's log in /s/gorgon/logs; run mse setup gorgon again.\n" +
			"The secrets key shown for gorgon was removed; if you saved it, delete it from your password manager."},
		"commit": {[]string{"config", "key"}, "broken\nNothing was kept apart from this run's log in /s/gorgon/logs; run mse setup gorgon again.\n" +
			"The secrets key shown for gorgon was removed; if you saved it, delete it from your password manager."},
	}
	for at, tt := range tests {
		t.Run(at, func(t *testing.T) {
			f, err := failingRun(t, stepFailure{at: at, err: errBroken})

			assert.ErrorIs(t, err, errBroken)
			assert.EqualError(t, err, tt.message)
			assert.Equal(t, tt.undone, f.undone)
		})
	}
}

func TestRunSaysQuittingKeptNothing(t *testing.T) {
	f, err := failingRun(t, stepFailure{at: "settings", err: configure.ErrAborted})

	assert.EqualError(t, err, "stopped before gorgon was created. Nothing was kept apart from this run's log in /s/gorgon/logs; run mse setup gorgon again.\nThe secrets key shown for gorgon was removed; if you saved it, delete it from your password manager.")
	assert.Equal(t, []string{"config", "key"}, f.undone)
}

func TestRunSaysAnInterruptionKeptNothing(t *testing.T) {
	f, err := failingRun(t, stepFailure{at: "commit", err: context.Canceled})

	assert.EqualError(t, err, "stopped before gorgon was created. Nothing was kept apart from this run's log in /s/gorgon/logs; run mse setup gorgon again.\nThe secrets key shown for gorgon was removed; if you saved it, delete it from your password manager.")
	assert.Equal(t, []string{"config", "key"}, f.undone)
}

func TestRunPointsATakenNameAtSetup(t *testing.T) {
	f, err := failingRun(t, stepFailure{at: "publish", err: NameTaken(errors.New("create media-server-config-gorgon: the repository already exists"))})

	assert.EqualError(t, err, "create media-server-config-gorgon: the repository already exists\n"+
		"Nothing was kept apart from this run's log in /s/gorgon/logs. media-server-config-gorgon exists, but this gh login can't see it: check gh auth status and --owner, or choose another name.\n"+
		"The secrets key shown for gorgon was removed; if you saved it, delete it from your password manager.")
	assert.Equal(t, []string{"config", "key"}, f.undone)
}

func TestRunDiscardsWhenPublishFailsBeforeCreating(t *testing.T) {
	f, err := failingRun(t, stepFailure{at: "publish", err: errBroken})

	assert.EqualError(t, err, "broken\nNothing was kept apart from this run's log in /s/gorgon/logs; run mse setup gorgon again.\nThe secrets key shown for gorgon was removed; if you saved it, delete it from your password manager.")
	assert.Equal(t, []string{"config", "key"}, f.undone)
}

func TestRunKeepsEverythingOnceTheRepositoryExists(t *testing.T) {
	tests := map[string]struct {
		failure stepFailure
		message string
	}{
		"push failed": {stepFailure{at: "publish", err: errBroken, created: true},
			"broken\ngorgon's repository exists and its config is committed in /c/gorgon, but not pushed. Finish with:\n  git -C /c/gorgon push --set-upstream origin main\n  mse backup --take-over --installation gorgon\n  mse apply --installation gorgon"},
		"main failed": {stepFailure{at: "main", err: errBroken},
			"broken\ngorgon is created and its config pushed. Finish with:\n  mse backup --take-over --installation gorgon\n  mse apply --installation gorgon"},
		"apply failed": {stepFailure{at: "apply", err: errBroken},
			"broken\ngorgon is created, its config pushed and this machine is its main. Finish with:\n  mse apply --installation gorgon"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f, err := failingRun(t, tt.failure)

			assert.EqualError(t, err, tt.message)
			assert.Empty(t, f.undone)
		})
	}
}

func TestRunNamesAKeyItCouldNotRemove(t *testing.T) {
	steps := newMockSteps(t)
	steps.EXPECT().CheckName(mock.Anything).Return(nil)
	steps.EXPECT().AddKey(mock.Anything).Return(func() error { return &KeyFileChangedError{Path: "/k/keys.txt", Name: "gorgon"} }, nil)
	steps.EXPECT().WriteConfig(mock.Anything).Return(func() error { return nil }, errBroken)
	var out bytes.Buffer

	err := Run(context.Background(), gorgon, steps, report.New(&out, &out, nil), func() func() { return func() {} })

	assert.EqualError(t, err, "broken\nThe rest was removed, but the new key stays in /k/keys.txt, which changed while mse ran; remove its lines (# media server gorgon) by hand. Then run mse setup gorgon again.")
}

func TestRunUndoesWithACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	steps := newMockSteps(t)
	steps.EXPECT().CheckName(mock.Anything).Return(nil)
	shielded, removed := false, false
	steps.EXPECT().AddKey(mock.Anything).Return(func() error { removed = shielded; return nil }, nil)
	steps.EXPECT().WriteConfig(mock.Anything).RunAndReturn(func(ctx context.Context) (Undo, error) {
		cancel()
		return nil, ctx.Err()
	})
	var out bytes.Buffer

	err := Run(ctx, gorgon, steps, report.New(&out, &out, nil), func() func() { shielded = true; return func() { shielded = false } })

	assert.EqualError(t, err, "stopped before gorgon was created. Nothing was kept; run mse setup gorgon again.\n"+
		"The secrets key shown for gorgon was removed; if you saved it, delete it from your password manager.")
	assert.True(t, removed, "the key was removed while signals were shielded")
}
