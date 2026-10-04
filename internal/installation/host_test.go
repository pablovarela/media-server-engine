package installation

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNetworkName(t *testing.T) {
	type Given struct {
		settings map[string]string
		host     Host
	}
	type Then struct {
		name string
		err  string
	}
	linux := Host{GOOS: "linux", Hostname: func() (string, error) { return "gorgon.lan", nil }}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"the setting wins": {
			Given: Given{settings: map[string]string{"MEDIA_SERVER_HOST": "media.example"}, host: linux},
			Then:  Then{name: "media.example"},
		},
		"linux: short hostname plus .local": {
			Given: Given{settings: map[string]string{}, host: linux},
			Then:  Then{name: "gorgon.local"},
		},
		"macOS: the local host name": {
			Given: Given{settings: map[string]string{}, host: Host{GOOS: "darwin", LocalHostName: func() (string, error) { return "Pablos-Mac", nil }}},
			Then:  Then{name: "Pablos-Mac.local"},
		},
		"hostname failing": {
			Given: Given{settings: map[string]string{}, host: Host{GOOS: "linux", Hostname: func() (string, error) { return "", errors.New("no hostname") }}},
			Then:  Then{err: "no hostname"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			loaded := &Installation{Settings: tt.Given.settings}

			name, err := loaded.NetworkName(tt.Given.host)

			if tt.Then.err != "" {
				assert.EqualError(t, err, tt.Then.err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.Then.name, name)
		})
	}
}
