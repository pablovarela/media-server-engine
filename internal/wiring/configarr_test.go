package wiring

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func configarrPrinting(output string, exit int) OneOff {
	return func(_ context.Context, out io.Writer) (int, error) {
		_, _ = io.WriteString(out, output)
		return exit, nil
	}
}

func TestConfigarrRunsOnceAndItsOutputGoesToTheLog(t *testing.T) {
	var tool bytes.Buffer
	env, _ := testEnv(t, nil, nil)

	require.NoError(t, Configarr(configarrPrinting("INFO done\n", 0), &tool)(context.Background(), env))

	assert.Equal(t, "INFO done\n", tool.String())
}

func TestErrorsConfigarrOnlyLogsStillFailTheStep(t *testing.T) {
	var tool bytes.Buffer
	env, _ := testEnv(t, nil, nil)
	output := "ERROR [13:53:14.981]: Create download client 'Deluge' failed\nINFO ok\nERROR two\nERROR three\nERROR four\n"

	err := Configarr(configarrPrinting(output, 0), &tool)(context.Background(), env)

	assert.EqualError(t, err, "configarr reported errors: ERROR [13:53:14.981]: Create download client 'Deluge' failed ERROR two ERROR three")
}

func TestConfigarrExitingNonZeroFailsTheStep(t *testing.T) {
	env, _ := testEnv(t, nil, nil)

	err := Configarr(configarrPrinting("", 3), io.Discard)(context.Background(), env)

	assert.EqualError(t, err, "configarr exited with 3")
}

func TestPasswordsInConfigarrsReportAreHidden(t *testing.T) {
	var tool bytes.Buffer
	env, _ := testEnv(t, nil, nil)
	output := "      fields.password: ******** -> hunter2-secret\n      fields.Password: old -> other-secret\n      fields.host: gluetun -> gluetun\nERROR password: leaked-in-error\n"

	err := Configarr(configarrPrinting(output, 0), &tool)(context.Background(), env)

	assert.Equal(t, "      fields.password: (hidden)\n      fields.Password: (hidden)\n      fields.host: gluetun -> gluetun\nERROR password: (hidden)\n", tool.String())
	assert.EqualError(t, err, "configarr reported errors: ERROR password: (hidden)")
}

func TestConfigarrThatCannotRunFailsTheStep(t *testing.T) {
	env, _ := testEnv(t, nil, nil)
	failing := func(context.Context, io.Writer) (int, error) { return 0, errors.New("no such service: configarr") }

	err := Configarr(failing, io.Discard)(context.Background(), env)

	assert.EqualError(t, err, "no such service: configarr")
}

func TestConfigarrsOutputHasTheAppSecretsHidden(t *testing.T) {
	var tool bytes.Buffer
	env, _ := testEnv(t, nil, map[string]string{"SONARR_API_KEY": "sonarr-key"})

	require.NoError(t, Configarr(configarrPrinting("connecting with sonarr-key\n", 0), &tool)(context.Background(), env))

	assert.Equal(t, "connecting with <hidden>\n", tool.String())
}
