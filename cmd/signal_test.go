package cmd

import (
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInterruptibleStopsOnSignals(t *testing.T) {
	type Given struct {
		signal syscall.Signal
	}
	tests := map[string]struct {
		Given Given
	}{
		"Ctrl-C":            {Given: Given{signal: syscall.SIGINT}},
		"stopped by a unit": {Given: Given{signal: syscall.SIGTERM}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, stop := interruptible()
			defer stop()

			require.NoError(t, syscall.Kill(syscall.Getpid(), tt.Given.signal))

			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("the context was not cancelled")
			}
		})
	}
}
