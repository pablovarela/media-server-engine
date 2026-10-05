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

			for _, p := range []*Painter{Stdout, Stderr} {
				assert.Equal(t, tt.Then.success, p.Success("ok"))
				assert.Equal(t, tt.Then.warning, p.Warning("ok"))
				assert.Equal(t, tt.Then.failure, p.Failure("ok"))
				assert.Equal(t, tt.Then.bold, p.Bold("ok"))
			}
		})
	}
}

func TestDetectDecidesEachStreamOnItsOwn(t *testing.T) {
	type Given struct {
		env                  map[string]string
		stdoutTTY, stderrTTY bool
	}
	type Then struct {
		stdout, stderr bool
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"both terminals":         {Given: Given{stdoutTTY: true, stderrTTY: true}, Then: Then{stdout: true, stderr: true}},
		"stdout piped":           {Given: Given{stderrTTY: true}, Then: Then{stderr: true}},
		"stderr redirected":      {Given: Given{stdoutTTY: true}, Then: Then{stdout: true}},
		"NO_COLOR turns it off":  {Given: Given{env: map[string]string{"NO_COLOR": "1"}, stdoutTTY: true, stderrTTY: true}},
		"a dumb terminal is off": {Given: Given{env: map[string]string{"TERM": "dumb"}, stdoutTTY: true, stderrTTY: true}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Cleanup(func() { Enable(false) })

			detect(func(key string) string { return tt.Given.env[key] }, tt.Given.stdoutTTY, tt.Given.stderrTTY)

			assert.Equal(t, tt.Then.stdout, Stdout.enabled)
			assert.Equal(t, tt.Then.stderr, Stderr.enabled)
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
