package cmd

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/wiring"
)

func TestAHangupInterrupts(t *testing.T) {
	ctx, stop := interruptible()
	defer stop()

	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGHUP))

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("a hangup did not interrupt")
	}
}

func TestShieldedSignalsDoNotEndTheProcess(t *testing.T) {
	release := shieldSignals()
	defer release()

	for _, signal := range []syscall.Signal{syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT} {
		require.NoError(t, syscall.Kill(os.Getpid(), signal))
	}
	time.Sleep(100 * time.Millisecond)
}

func TestTheFirstSignalStillInterruptsWhileShielded(t *testing.T) {
	ctx, stop := interruptible()
	defer stop()
	release := shieldSignals()
	defer release()

	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGTERM))

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("a shielded signal did not interrupt")
	}
	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGTERM))
	time.Sleep(100 * time.Millisecond)
}

func TestSignalsStillInterruptAfterConfigarrRuns(t *testing.T) {
	ctx, stop := interruptible()
	defer stop()
	f := newApplyFixture(t)
	f.expectApply(nil)
	f.composer.EXPECT().RunOnce(mock.Anything, f.project, "configarr").RunAndReturn(func(context.Context, *types.Project, string) (int, error) {
		signal.Reset()
		return 0, nil
	})
	deps := f.deps(t, false)
	deps.WiringSteps = func(configarr wiring.OneOff, tool io.Writer) ([]wiring.Step, error) {
		return []wiring.Step{{Name: "configarr", Run: wiring.Configarr(configarr, tool)}}, nil
	}
	root := NewRootCommand(deps)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	require.Equal(t, 0, run(ctx, root, []string{"apply"}))

	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGHUP))

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("a hangup after configarr did not interrupt")
	}
}
