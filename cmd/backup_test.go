package cmd

import (
	"bytes"
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/process"
)

func TestBackupTakeOverRefusals(t *testing.T) {
	asksAboutTheMain := func(r *mockCommandRunner, c *mockComposeRunner) {
		c.EXPECT().Load(mock.Anything, mock.Anything, compose.Stack, mock.Anything, mock.Anything).Return(&types.Project{Name: "media-server"}, nil).Once()
		r.EXPECT().Output(mock.Anything, resticCall("cat", "config", "--no-lock")).Return(process.Result{}, nil).Once()
		r.EXPECT().Output(mock.Anything, resticCall("snapshots", "--no-lock", "--host", "gorgon", "--json")).Return(process.Result{Stdout: []byte(theirSnapshots)}, nil).Once()
	}
	type Given struct {
		interactive bool
		stdin       string
		expect      func(r *mockCommandRunner, c *mockComposeRunner)
	}
	type When struct {
		args []string
	}
	type Then struct {
		stderr string
	}
	tests := map[string]struct {
		Given Given
		When  When
		Then  Then
	}{
		"declined": {
			Given: Given{interactive: true, stdin: "n\n", expect: asksAboutTheMain},
			When:  When{args: []string{"backup", "--take-over"}},
			Then:  Then{stderr: "mse: nothing was claimed\n"},
		},
		"no one to answer": {
			Given: Given{expect: asksAboutTheMain},
			When:  When{args: []string{"backup", "--take-over"}},
			Then:  Then{stderr: "mse: nothing was claimed; mse backup --take-over --yes takes over without asking\n"},
		},
		"--yes without --take-over": {
			When: When{args: []string{"backup", "--yes"}},
			Then: Then{stderr: "mse: --yes only goes with --take-over\n"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			home, _, _, tmp := backupHome(t)
			runner := newMockCommandRunner(t)
			composer := newMockComposeRunner(t)
			if tt.Given.expect != nil {
				tt.Given.expect(runner, composer)
			}
			var pings []string
			root := NewRootCommand(backupDependencies(t, home, tmp, runner, composer, tt.Given.interactive, &pings))
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetIn(strings.NewReader(tt.Given.stdin))

			code := run(context.Background(), root, tt.When.args)

			assert.Equal(t, 1, code)
			assert.True(t, strings.HasSuffix(stderr.String(), tt.Then.stderr), stderr.String())
			assert.Empty(t, pings)
		})
	}
}

func localRestic(context.Context) (string, error) { return "restic", nil }

func resticCall(args ...string) any {
	return mock.MatchedBy(func(c process.Command) bool {
		return c.Name == "restic" && slices.Equal(args, c.Args) &&
			slices.Contains(c.Env, "RESTIC_REPOSITORY=b2:bucket") && slices.Contains(c.Env, "RESTIC_PASSWORD=secret")
	})
}

func TestBackupCommandsNeedARepository(t *testing.T) {
	getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
	root := NewRootCommand(Dependencies{
		ResticBinary: localRestic,
		Environment:  getenv, Home: home, Update: newMockUpdater(t),
		Decrypt: func(string) ([]byte, error) { return []byte("RESTIC_PASSWORD=secret\n"), nil },
		Host:    installation.Host{GOOS: "linux", Hostname: func() (string, error) { return "gorgon", nil }},
		Run:     func(_, _ io.Writer) commandRunner { return newMockCommandRunner(t) },
	})
	var stderr bytes.Buffer
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"check-backup"})

	assert.Equal(t, 1, code)
	assert.Equal(t, "mse: gorgon has no backup repository: set RESTIC_REPOSITORY in installation.env\n", stderr.String())
}
