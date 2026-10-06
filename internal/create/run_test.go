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

var gorgon = Installation{Name: "gorgon", Config: "/c/gorgon", Data: "/d/gorgon"}

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
	f.steps.EXPECT().CheckMachine(mock.Anything).RunAndReturn(func(context.Context) error { return fails("machine") }).Maybe()
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
	err := Run(context.Background(), gorgon, f.steps, report.New(&f.out, &f.out, nil), func() func() { return func() {} })
	return f, err
}

func TestRunGoesThroughEveryStep(t *testing.T) {
	f, err := failingRun(t, stepFailure{})

	assert.NoError(t, err)
	assert.Equal(t, []string{"machine", "name", "key", "config", "settings", "commit", "publish", "main", "apply"}, f.order)
	assert.Empty(t, f.undone)
	assert.Contains(t, f.out.String(), "gorgon is ready. Its config is in /c/gorgon and its data in /d/gorgon; mse urls lists the apps' addresses.")
}

var errBroken = errors.New("broken")

func TestRunStopsBeforeWritingAnything(t *testing.T) {
	for _, at := range []string{"machine", "name"} {
		t.Run(at, func(t *testing.T) {
			f, err := failingRun(t, stepFailure{at: at, err: errBroken})

			assert.ErrorIs(t, err, errBroken)
			assert.Empty(t, f.undone)
			assert.Equal(t, at, f.order[len(f.order)-1])
		})
	}
}

func TestRunUndoesWhatItWrote(t *testing.T) {
	tests := map[string][]string{
		"key":      {"key"},
		"config":   {"config", "key"},
		"settings": {"config", "key"},
		"commit":   {"config", "key"},
	}
	for at, undone := range tests {
		t.Run(at, func(t *testing.T) {
			f, err := failingRun(t, stepFailure{at: at, err: errBroken})

			assert.ErrorIs(t, err, errBroken)
			assert.EqualError(t, err, "broken\nNothing was kept; run mse create gorgon again.")
			assert.Equal(t, undone, f.undone)
		})
	}
}

func TestRunSaysQuittingKeptNothing(t *testing.T) {
	f, err := failingRun(t, stepFailure{at: "settings", err: configure.ErrAborted})

	assert.EqualError(t, err, "stopped before gorgon was created. Nothing was kept; run mse create gorgon again.")
	assert.Equal(t, []string{"config", "key"}, f.undone)
}

func TestRunDiscardsWhenPublishFailsBeforeCreating(t *testing.T) {
	f, err := failingRun(t, stepFailure{at: "publish", err: errBroken})

	assert.EqualError(t, err, "broken\nNothing was kept; run mse create gorgon again.")
	assert.Equal(t, []string{"config", "key"}, f.undone)
}

func TestRunKeepsEverythingOnceTheRepositoryExists(t *testing.T) {
	tests := map[string]struct {
		failure stepFailure
		message string
	}{
		"push failed": {stepFailure{at: "publish", err: errBroken, created: true},
			"broken\ngorgon's repository exists and its config is committed in /c/gorgon, but not pushed. Finish with:\n  git -C /c/gorgon push --set-upstream origin main\n  mse claim-backup-main\n  mse apply"},
		"main failed": {stepFailure{at: "main", err: errBroken},
			"broken\ngorgon is created and its config pushed. Finish with:\n  mse claim-backup-main\n  mse apply"},
		"apply failed": {stepFailure{at: "apply", err: errBroken},
			"broken\ngorgon is created, its config pushed and this machine is its main. Finish with:\n  mse apply"},
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
	steps.EXPECT().CheckMachine(mock.Anything).Return(nil)
	steps.EXPECT().CheckName(mock.Anything).Return(nil)
	steps.EXPECT().AddKey(mock.Anything).Return(func() error { return &KeyFileChangedError{Path: "/k/keys.txt", Name: "gorgon"} }, nil)
	steps.EXPECT().WriteConfig(mock.Anything).Return(func() error { return nil }, errBroken)
	var out bytes.Buffer

	err := Run(context.Background(), gorgon, steps, report.New(&out, &out, nil), func() func() { return func() {} })

	assert.EqualError(t, err, "broken\nThe rest was removed, but the new key stays in /k/keys.txt, which changed while mse create ran; remove its lines (# media server gorgon) by hand. Then run mse create gorgon again.")
}

func TestRunUndoesWithACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	steps := newMockSteps(t)
	steps.EXPECT().CheckMachine(mock.Anything).Return(nil)
	steps.EXPECT().CheckName(mock.Anything).Return(nil)
	shielded, removed := false, false
	steps.EXPECT().AddKey(mock.Anything).Return(func() error { removed = shielded; return nil }, nil)
	steps.EXPECT().WriteConfig(mock.Anything).RunAndReturn(func(ctx context.Context) (Undo, error) {
		cancel()
		return nil, ctx.Err()
	})
	var out bytes.Buffer

	err := Run(ctx, gorgon, steps, report.New(&out, &out, nil), func() func() { shielded = true; return func() { shielded = false } })

	assert.ErrorIs(t, err, context.Canceled)
	assert.True(t, removed, "the key was removed while signals were shielded")
}
