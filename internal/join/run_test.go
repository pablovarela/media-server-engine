package join

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/pablovarela/media-server-engine/internal/configure"
	"github.com/pablovarela/media-server-engine/internal/create"
	"github.com/pablovarela/media-server-engine/internal/report"
)

var gorgon = Installation{Name: "gorgon", Logs: "/s/gorgon/logs"}

var errBroken = errors.New("broken")

type stepFailure struct {
	at  string
	err error
}

type runFixture struct {
	steps         *mockSteps
	order, undone []string
	out           bytes.Buffer
}

func failingRun(t *testing.T, failure stepFailure) (*runFixture, error) {
	f := &runFixture{steps: newMockSteps(t)}
	undo := func(name string) create.Undo { return func() error { f.undone = append(f.undone, name); return nil } }
	fails := func(name string) error {
		f.order = append(f.order, name)
		if name == failure.at {
			return failure.err
		}
		return nil
	}
	f.steps.EXPECT().CheckMachine(mock.Anything).RunAndReturn(func(context.Context) error { return fails("machine") }).Maybe()
	f.steps.EXPECT().CheckName(mock.Anything).RunAndReturn(func(context.Context) error { return fails("name") }).Maybe()
	f.steps.EXPECT().Clone(mock.Anything).RunAndReturn(func(context.Context) (create.Undo, error) { return undo("clone"), fails("clone") }).Maybe()
	f.steps.EXPECT().AddKey(mock.Anything).RunAndReturn(func(context.Context) (create.Undo, error) { return undo("key"), fails("key") }).Maybe()
	f.steps.EXPECT().Data(mock.Anything).RunAndReturn(func(context.Context) error { return fails("data") }).Maybe()
	f.steps.EXPECT().Role(mock.Anything).RunAndReturn(func(context.Context) error { return fails("role") }).Maybe()
	f.steps.EXPECT().Apply(mock.Anything).RunAndReturn(func(context.Context) error { return fails("apply") }).Maybe()
	f.steps.EXPECT().Summary(mock.Anything).Return("gorgon is running here.").Maybe()
	err := Run(context.Background(), gorgon, f.steps, report.New(&f.out, &f.out, nil), func() func() { return func() {} })
	return f, err
}

func TestRunGoesThroughEveryStep(t *testing.T) {
	f, err := failingRun(t, stepFailure{})

	assert.NoError(t, err)
	assert.Equal(t, []string{"machine", "name", "clone", "key", "data", "role", "apply"}, f.order)
	assert.Empty(t, f.undone)
	assert.Contains(t, f.out.String(), "gorgon is running here.\n")
}

func TestRunStopsBeforeWritingAnything(t *testing.T) {
	for _, at := range []string{"machine", "name"} {
		t.Run(at, func(t *testing.T) {
			f, err := failingRun(t, stepFailure{at: at, err: errBroken})

			assert.Equal(t, errBroken, err)
			assert.Empty(t, f.undone)
		})
	}
}

func TestRunUndoesBeforeTheRestore(t *testing.T) {
	tests := map[string]struct {
		undone  []string
		message string
	}{
		"clone": {[]string{"clone"}, "broken\nNothing was kept apart from this run's log in /s/gorgon/logs; run mse join gorgon again."},
		"key":   {[]string{"key", "clone"}, "broken\nNothing was kept apart from this run's log in /s/gorgon/logs; run mse join gorgon again."},
	}
	for at, tt := range tests {
		t.Run(at, func(t *testing.T) {
			f, err := failingRun(t, stepFailure{at: at, err: errBroken})

			assert.EqualError(t, err, tt.message)
			assert.Equal(t, tt.undone, f.undone)
		})
	}
}

func TestRunSaysStoppedWhenQuitBeforeTheRestore(t *testing.T) {
	for name, err := range map[string]error{"esc": configure.ErrAborted, "Ctrl+C": context.Canceled} {
		t.Run(name, func(t *testing.T) {
			f, got := failingRun(t, stepFailure{at: "key", err: err})

			assert.EqualError(t, got, "stopped before gorgon joined. Nothing was kept apart from this run's log in /s/gorgon/logs; run mse join gorgon again.")
			assert.Equal(t, []string{"key", "clone"}, f.undone)
		})
	}
}

func TestRunNamesAKeyItCouldNotRemove(t *testing.T) {
	steps := newMockSteps(t)
	steps.EXPECT().CheckMachine(mock.Anything).Return(nil)
	steps.EXPECT().CheckName(mock.Anything).Return(nil)
	steps.EXPECT().Clone(mock.Anything).Return(func() error { return nil }, nil)
	steps.EXPECT().AddKey(mock.Anything).Return(func() error { return &create.KeyFileChangedError{Path: "/k/keys.txt", Name: "gorgon"} }, errBroken)
	var out bytes.Buffer

	err := Run(context.Background(), gorgon, steps, report.New(&out, &out, nil), func() func() { return func() {} })

	assert.EqualError(t, err, "broken\nThe rest was removed, but the new key stays in /k/keys.txt, which changed while mse ran; "+
		"remove its lines (# media server gorgon) by hand. Then run mse join gorgon again.")
}

func TestRunNeverUndoesFromTheRestoreOn(t *testing.T) {
	steps := newMockSteps(t)
	steps.EXPECT().CheckMachine(mock.Anything).Return(nil)
	steps.EXPECT().CheckName(mock.Anything).Return(nil)
	steps.EXPECT().Clone(mock.Anything).Return(func() error { t.Error("the clone was undone"); return nil }, nil)
	steps.EXPECT().AddKey(mock.Anything).Return(func() error { t.Error("the key was undone"); return nil }, nil)
	steps.EXPECT().Data(mock.Anything).Return(errBroken)
	var out bytes.Buffer

	err := Run(context.Background(), gorgon, steps, report.New(&out, &out, nil), func() func() { return func() {} })

	assert.ErrorIs(t, err, errBroken)
}

func TestRunKeepsEverythingFromTheRestoreOn(t *testing.T) {
	tests := map[string]struct {
		failure stepFailure
		message string
	}{
		"restore failed": {stepFailure{at: "data", err: errBroken}, "broken\n" +
			"gorgon's config and key are on this machine; its data isn't restored. Finish with:\n" +
			"  mse restore --overwrite --installation gorgon\n  mse claim-backup-main --installation gorgon (to make this machine the main)\n  mse apply --installation gorgon"},
		"restore interrupted": {stepFailure{at: "data", err: context.Canceled}, "stopped while restoring gorgon's data. Its config and key are on this machine. Finish with:\n" +
			"  mse restore --overwrite --installation gorgon\n  mse claim-backup-main --installation gorgon (to make this machine the main)\n  mse apply --installation gorgon"},
		"claim failed": {stepFailure{at: "role", err: errBroken}, "broken\n" +
			"gorgon's config, key and data are on this machine. Finish with:\n" +
			"  mse claim-backup-main --installation gorgon (to make this machine the main)\n  mse apply --installation gorgon"},
		"apply failed": {stepFailure{at: "apply", err: errBroken}, "broken\n" +
			"gorgon's config, key and data are on this machine and its role is set. Finish with:\n  mse apply --installation gorgon"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f, err := failingRun(t, tt.failure)

			assert.EqualError(t, err, tt.message)
			assert.Empty(t, f.undone)
		})
	}
}

func TestRunKeepsEverythingWhenTheMainQuestionIsQuit(t *testing.T) {
	f, err := failingRun(t, stepFailure{at: "role", err: configure.ErrAborted})

	assert.EqualError(t, err, "stopped before choosing this machine's role. gorgon's config, key and data are on this machine. Finish with:\n"+
		"  mse claim-backup-main --installation gorgon (to make this machine the main)\n  mse apply --installation gorgon")
	assert.Empty(t, f.undone)
}

func TestRunUndoesWithACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	steps := newMockSteps(t)
	steps.EXPECT().CheckMachine(mock.Anything).Return(nil)
	steps.EXPECT().CheckName(mock.Anything).Return(nil)
	shielded, removed := false, false
	steps.EXPECT().Clone(mock.Anything).Return(func() error { removed = shielded; return nil }, nil)
	steps.EXPECT().AddKey(mock.Anything).RunAndReturn(func(ctx context.Context) (create.Undo, error) {
		cancel()
		return nil, ctx.Err()
	})
	var out bytes.Buffer

	err := Run(ctx, gorgon, steps, report.New(&out, &out, nil), func() func() { shielded = true; return func() { shielded = false } })

	assert.EqualError(t, err, "stopped before gorgon joined. Nothing was kept apart from this run's log in /s/gorgon/logs; run mse join gorgon again.")
	assert.True(t, removed, "the clone was removed while signals were shielded")
}
