package apply

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/pablovarela/media-server-engine/internal/report"
)

func startingStack(t *testing.T, ctx context.Context) *mockStack {
	t.Helper()
	stack := newMockStack(t)
	stack.EXPECT().BindSources().Return(nil)
	stack.EXPECT().Pull(ctx).Return("pulled 0 images", nil)
	stack.EXPECT().Up(ctx).Return("up to date", nil)
	stack.EXPECT().Reattach(ctx).Return("nothing to reattach", nil)
	return stack
}

func TestTheForwardedPortIsSyncedAfterTheWiring(t *testing.T) {
	type Given struct {
		result  string
		err     error
		wireErr error
	}
	type Then struct {
		err    string
		stdout string
		stderr string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"Deluge moves to the port": {
			Given: Given{result: "moved Deluge to forwarded port 51234"},
			Then:  Then{stdout: "Wiring the apps... nothing to change.\nForwarding the VPN port... moved Deluge to forwarded port 51234.\n"},
		},
		"no port yet only warns": {
			Given: Given{err: errors.New("the VPN has no forwarded port; downloads continue without incoming connections")},
			Then: Then{
				stdout: "Wiring the apps... nothing to change.\nForwarding the VPN port... not now.\n",
				stderr: "port forwarding: the VPN has no forwarded port; downloads continue without incoming connections",
			},
		},
		"a failed wiring still syncs the port": {
			Given: Given{result: "Deluge listens on forwarded port 51234", wireErr: errors.New("wiring failed: seerr")},
			Then:  Then{err: "wiring failed: seerr", stdout: "Forwarding the VPN port... Deluge listens on forwarded port 51234.\n"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			wiring := newMockWiring(t)
			wiring.EXPECT().Wire(ctx).Return("nothing to change", tt.Given.wireErr)
			forwarding := newMockForwarding(t)
			forwarding.EXPECT().Sync(ctx).Return(tt.Given.result, tt.Given.err)
			page := newMockPage(t)
			page.EXPECT().Reload(ctx).Return(nil)
			images := newMockImages(t)
			if tt.Given.wireErr == nil {
				images.EXPECT().Prune(ctx).Return(nil)
			}
			var stdout, stderr bytes.Buffer

			err := (&Apply{
				Stack: startingStack(t, ctx), Wiring: wiring, Forwarding: forwarding, Page: page, Images: images,
				MkdirAll: func(string) error { return nil }, Sleep: func(context.Context, time.Duration) error { return nil },
				Report: report.New(&stdout, &stderr, nil),
			}).Run(ctx)

			if tt.Then.err == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.Then.err)
			}
			assert.Contains(t, stdout.String(), tt.Then.stdout)
			assert.Contains(t, stderr.String(), tt.Then.stderr)
		})
	}
}

func TestAStackKeptStoppedSyncsNoPort(t *testing.T) {
	ctx := context.Background()
	stack := newMockStack(t)
	stack.EXPECT().BindSources().Return(nil)
	stack.EXPECT().Pull(ctx).Return("pulled 0 images", nil)
	var stdout bytes.Buffer

	err := (&Apply{
		Stack: stack, Forwarding: newMockForwarding(t), KeepStopped: "the media is still to be restored",
		MkdirAll: func(string) error { return nil }, Report: report.New(&stdout, &stdout, nil),
	}).Run(ctx)

	assert.NoError(t, err)
	assert.NotContains(t, stdout.String(), "Forwarding the VPN port")
}
