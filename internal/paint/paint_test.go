package paint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPaint(t *testing.T) {
	type Given struct {
		enabled bool
	}
	type Then struct {
		success, warning, failure, bold string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"plain by default": {
			Then: Then{success: "ok", warning: "ok", failure: "ok", bold: "ok"},
		},
		"coloured on a terminal": {
			Given: Given{enabled: true},
			Then:  Then{success: "\x1b[32mok\x1b[0m", warning: "\x1b[33mok\x1b[0m", failure: "\x1b[31mok\x1b[0m", bold: "\x1b[1mok\x1b[22m"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			Enable(tt.Given.enabled)
			t.Cleanup(func() { Enable(false) })

			assert.Equal(t, tt.Then.success, Success("ok"))
			assert.Equal(t, tt.Then.warning, Warning("ok"))
			assert.Equal(t, tt.Then.failure, Failure("ok"))
			assert.Equal(t, tt.Then.bold, Bold("ok"))
		})
	}
}

func TestServiceColoursAreStablePerName(t *testing.T) {
	Enable(true)
	t.Cleanup(func() { Enable(false) })
	services := Services{}

	first, second, again := services.Paint("sonarr"), services.Paint("radarr"), services.Paint("sonarr")

	assert.Equal(t, "\x1b[36msonarr\x1b[0m", first)
	assert.Equal(t, "\x1b[33mradarr\x1b[0m", second)
	assert.Equal(t, first, again)
}
