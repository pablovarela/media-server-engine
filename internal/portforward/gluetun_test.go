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
		"a forwarded port":           {Given: Given{output: `{"port":51234,"ports":[51234]}`}, Then: Then{port: 51234}},
		"an older gluetun":           {Given: Given{output: `{"port":51234}`}, Then: Then{port: 51234}},
		"a gluetun without port":     {Given: Given{output: `{"ports":[51234]}`}, Then: Then{port: 51234}},
		"no forwarded port":          {Given: Given{output: `{"port":0,"ports":[]}`}, Then: Then{err: ErrNoPort}},
		"an unreadable answer":       {Given: Given{output: "<html>"}, Then: Then{err: ErrGluetun}},
		"the control server is down": {Given: Given{err: errors.New("wget: unable to connect to remote host: Connection refused")}, Then: Then{err: ErrGluetun}},
		"gluetun is down":            {Given: Given{err: errors.New("container 1a2b is not running")}, Then: Then{err: ErrGluetunStopped}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			docker := newMockDocker(t)
			docker.EXPECT().Exec(context.Background(), "gluetun", "", []string{"wget", "-qO-", "--header", "X-API-Key: control-key", "http://127.0.0.1:8000/v1/portforward"}).Return(tt.Given.output, tt.Given.err)

			port, err := Gluetun{Docker: docker, Key: "control-key"}.Port(context.Background())

			assert.Equal(t, tt.Then.port, port)
			if tt.Then.err == nil {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tt.Then.err)
		})
	}
}

func TestGluetunErrorsDoNotShowTheKey(t *testing.T) {
	docker := newMockDocker(t)
	docker.EXPECT().Exec(context.Background(), "gluetun", "", []string{"wget", "-qO-", "--header", "X-API-Key: control-key", "http://127.0.0.1:8000/v1/portforward"}).
		Return("", errors.New("wget --header X-API-Key: control-key failed"))

	_, err := Gluetun{Docker: docker, Key: "control-key"}.Port(context.Background())

	assert.NotContains(t, err.Error(), "control-key")
}
