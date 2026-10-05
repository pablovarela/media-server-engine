package wiring

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValuesPrintAsPythonPrintsThem(t *testing.T) {
	tests := map[string]struct {
		value any
		want  string
	}{
		"none":              {nil, "None"},
		"true":              {true, "True"},
		"a JSON integer":    {float64(25), "25"},
		"a small float":     {0.00001, "1e-05"},
		"a Python float":    {pythonFloat(2000), "2000.0"},
		"a tiny float":      {pythonFloat(0.00001), "1e-05"},
		"a huge float":      {pythonFloat(1e16), "1e+16"},
		"a list of numbers": {[]any{float64(5000), float64(5010)}, "[5000, 5010]"},
		"a list of strings": {[]any{"a", "b"}, "['a', 'b']"},
		"a quote inside":    {[]any{"it's"}, `["it's"]`},
		"a dict":            {map[string]any{"b": true, "a": "x"}, "{'a': 'x', 'b': True}"},
		"a string":          {"http://sonarr:8989", "http://sonarr:8989"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, show(tt.value))
		})
	}
}
