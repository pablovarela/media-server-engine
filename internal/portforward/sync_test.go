package portforward

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/wiring"
)

func TestForward(t *testing.T) {
	type Given struct {
		gluetunPort   int
		gluetunErr    error
		before, after int
	}
	type Then struct {
		result Result
		err    error
		marked bool
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"Deluge moves to the port":     {Given: Given{gluetunPort: 51234, before: 6881, after: 51234}, Then: Then{result: Result{Port: 51234, Moved: true}, marked: true}},
		"Deluge is already on it":      {Given: Given{gluetunPort: 51234, before: 51234}, Then: Then{result: Result{Port: 51234}, marked: true}},
		"Deluge doesn't take the port": {Given: Given{gluetunPort: 51234, before: 6881, after: 6881}, Then: Then{err: ErrNotMoved, marked: true}},
		"no forwarded port":            {Given: Given{gluetunErr: ErrNoPort}, Then: Then{err: ErrNoPort}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			ports := newMockPorts(t)
			ports.EXPECT().Port(ctx).Return(tt.Given.gluetunPort, tt.Given.gluetunErr)
			deluge := newMockListener(t)
			if tt.Given.gluetunErr == nil {
				deluge.EXPECT().ListenPort(ctx).Return(tt.Given.before, nil).Once()
			}
			if tt.Given.after != 0 {
				deluge.EXPECT().Listen(ctx, tt.Given.gluetunPort).Return(nil)
				deluge.EXPECT().ListenPort(ctx).Return(tt.Given.after, nil).Once()
			}
			state := wiring.State{Dir: t.TempDir()}

			result, err := Sync{Gluetun: ports, Deluge: deluge, State: state}.Forward(ctx)

			assert.Equal(t, tt.Then.result, result)
			if tt.Then.err == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tt.Then.err)
			}
			assert.Equal(t, tt.Then.marked, state.Remembered(Marker) == "yes")
		})
	}
}

func TestRestore(t *testing.T) {
	tests := map[string]struct {
		marked     bool
		randomErr  error
		restored   bool
		err        string
		markedThen bool
	}{
		"Deluge's port was forwarded": {marked: true, restored: true},
		"it never was":                {},
		"Deluge refuses":              {marked: true, randomErr: errors.New("the Deluge web UI isn't answering"), err: "the Deluge web UI isn't answering", markedThen: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			state := wiring.State{Dir: t.TempDir()}
			if tt.marked {
				require.NoError(t, state.Remember(Marker, "yes"))
			}
			deluge := newMockListener(t)
			if tt.marked {
				deluge.EXPECT().RandomPort(ctx).Return(tt.randomErr)
			}

			restored, err := Sync{Deluge: deluge, State: state}.Restore(ctx)

			assert.Equal(t, tt.restored, restored)
			if tt.err == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.err)
			}
			assert.Equal(t, tt.markedThen, state.Remembered(Marker) == "yes")
		})
	}
}

func TestTheResultSaysWhatHappened(t *testing.T) {
	assert.Equal(t, "moved Deluge to forwarded port 51234", Result{Port: 51234, Moved: true}.String())
	assert.Equal(t, "Deluge listens on forwarded port 51234", Result{Port: 51234}.String())
}
