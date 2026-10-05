package process

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOutput(t *testing.T) {
	type Given struct {
		command Command
	}
	type Then struct {
		result Result
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"stdout, stderr and the exit code": {
			Given: Given{command: Command{Name: "sh", Args: []string{"-c", "echo out; echo err >&2; exit 3"}}},
			Then:  Then{result: Result{Stdout: []byte("out\n"), Stderr: []byte("err\n"), Exit: 3}},
		},
		"the environment is added": {
			Given: Given{command: Command{Name: "sh", Args: []string{"-c", `printf %s "$MSE_RUN_TEST"`}, Env: []string{"MSE_RUN_TEST=hello"}}},
			Then:  Then{result: Result{Stdout: []byte("hello"), Stderr: []byte{}}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := System{}.Output(context.Background(), tt.Given.command)

			require.NoError(t, err)
			assert.Equal(t, string(tt.Then.result.Stdout), string(result.Stdout))
			assert.Equal(t, string(tt.Then.result.Stderr), string(result.Stderr))
			assert.Equal(t, tt.Then.result.Exit, result.Exit)
		})
	}
}

func TestRunStreamsInTheDirectory(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	var out, errOut bytes.Buffer

	exit, err := System{Out: &out, ErrOut: &errOut}.Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "pwd; echo warning >&2"}, Dir: dir})

	require.NoError(t, err)
	assert.Equal(t, 0, exit)
	assert.Equal(t, dir+"\n", out.String())
	assert.Equal(t, "warning\n", errOut.String())
}

func TestRunPassesExtraFiles(t *testing.T) {
	held, err := os.Create(filepath.Join(t.TempDir(), "held"))
	require.NoError(t, err)
	defer func() { _ = held.Close() }()

	exit, err := System{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}.Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "echo inherited >&3"}, ExtraFiles: []*os.File{held}})

	require.NoError(t, err)
	assert.Equal(t, 0, exit)
	text, err := os.ReadFile(held.Name())
	require.NoError(t, err)
	assert.Equal(t, "inherited\n", string(text))
}

func TestRunErrors(t *testing.T) {
	t.Run("a missing program", func(t *testing.T) {
		_, err := System{}.Run(context.Background(), Command{Name: "mse-no-such-program"})

		assert.ErrorContains(t, err, "mse-no-such-program")
	})
	t.Run("an interrupt", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_, err := System{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}.Run(ctx, Command{Name: "sleep", Args: []string{"10"}})

		assert.EqualError(t, err, "sleep was interrupted")
	})
}

func TestTheWorkingDirectoryIsThePWD(t *testing.T) {
	dir := t.TempDir()

	result, err := System{}.Output(context.Background(), Command{Name: "sh", Args: []string{"-c", `printf %s "$PWD"`}, Dir: dir, Env: []string{"PWD=/elsewhere"}})

	require.NoError(t, err)
	assert.Equal(t, dir, string(result.Stdout))
}
