package portforward

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnabled(t *testing.T) {
	tests := map[string]struct {
		settings map[string]string
		enabled  bool
		err      string
	}{
		"not set": {settings: map[string]string{}},
		"empty":   {settings: map[string]string{Setting: ""}},
		"no":      {settings: map[string]string{Setting: "no"}},
		"yes":     {settings: map[string]string{Setting: "yes"}, enabled: true},
		"on":      {settings: map[string]string{Setting: "on"}, err: "VPN_PORT_FORWARDING: must be yes or no"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			enabled, err := Enabled(tt.settings)

			if tt.err != "" {
				assert.EqualError(t, err, tt.err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.enabled, enabled)
		})
	}
}
