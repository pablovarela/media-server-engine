package portforward

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGluetunPort(t *testing.T) {
	type Given struct {
		output string
		err    error
	}
	type Then struct {
		port int
		err  error
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"a forwarded port": {Given: Given{output: "51234\n"}, Then: Then{port: 51234}},
		"an empty file":    {Given: Given{output: ""}, Then: Then{err: ErrNoPort}},
		"port zero":        {Given: Given{output: "0\n"}, Then: Then{err: ErrNoPort}},
		"not a number":     {Given: Given{output: "abc\n"}, Then: Then{err: ErrNoPort}},
		"no file":          {Given: Given{err: errors.New("cat: can't open '/tmp/gluetun/forwarded_port': No such file or directory")}, Then: Then{err: ErrNoPort}},
		"gluetun is down":  {Given: Given{err: errors.New("container 1a2b is not running")}, Then: Then{err: ErrGluetun}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			docker := newMockDocker(t)
			docker.EXPECT().Exec(context.Background(), "gluetun", "", []string{"cat", "/tmp/gluetun/forwarded_port"}).Return(tt.Given.output, tt.Given.err)

			port, err := Gluetun{Docker: docker}.Port(context.Background())

			assert.Equal(t, tt.Then.port, port)
			if tt.Then.err == nil {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tt.Then.err)
		})
	}
}
