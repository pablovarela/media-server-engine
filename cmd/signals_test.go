package cmd

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
