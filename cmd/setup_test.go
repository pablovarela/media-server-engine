package cmd

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestSetupCreatesOnlyAfterAYes(t *testing.T) {
	type Given struct {
		owner  string
		answer bool
	}
	type Then struct {
		question string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"under the gh user": {
			Then: Then{question: "There's no media-server-config-gorgon under pablovarela. Create a new installation called gorgon?"},
		},
		"under an organisation": {
			Given: Given{owner: "acme"},
			Then:  Then{question: "There's no media-server-config-gorgon under acme. Create a new installation called gorgon?"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newCreateFixture(t)
			owner := "pablovarela"
			args := []string{"gorgon"}
			if tt.Given.owner != "" {
				owner = tt.Given.owner
				args = append(args, "--owner", owner)
			}
			f.repositories.EXPECT().Login(mock.Anything).Return("pablovarela", nil).Once()
			f.repositories.EXPECT().RepositoryExists(mock.Anything, owner, "media-server-config-gorgon").Return(false, nil).Once()
			f.prompter.EXPECT().Ask(tt.Then.question, false).Return(tt.Given.answer, nil).Once()

			code, _, stderr := f.create(t, args...)

			assert.Equal(t, 1, code)
			assert.Equal(t, "mse: nothing was created\n", stderr)
			f.nothingKept(t)
		})
	}
}

func TestSetupSaysWhichInstallationItRebuilds(t *testing.T) {
	f := newJoinFixture(t)
	f.repositoryExists()
	f.prompter.EXPECT().Secret(mock.Anything, mock.Anything, mock.Anything).Return("", context.Canceled).Once()

	_, stdout, _ := f.join(t, "gorgon")

	assert.Contains(t, stdout, "Rebuilding gorgon from pablovarela/media-server-config-gorgon.\n")
}

func TestSetupRefusesAHomepagePortForARebuild(t *testing.T) {
	f := newJoinFixture(t)
	f.repositoryExists()

	code, _, stderr := f.join(t, "gorgon", "--homepage-port", "8080")

	assert.Equal(t, 1, code)
	assert.Equal(t, "mse: --homepage-port only applies to a new installation; gorgon's port comes from its config\n", stderr)
	assert.NoDirExists(t, f.config)
}

func TestSetupRefusesAnInstallationAlreadySetUp(t *testing.T) {
	f := newJoinFixture(t)
	require.NoError(t, os.MkdirAll(f.config, 0o755))

	code, _, stderr := f.join(t, "gorgon")

	assert.Equal(t, 1, code)
	assert.Equal(t, "mse: gorgon is already set up on this machine; mse status shows its state\n", stderr)
}

func TestCreateAndJoinAreGone(t *testing.T) {
	for _, command := range []string{"create", "join"} {
		t.Run(command, func(t *testing.T) {
			getenv, home := xdgHome(t, map[string]string{})
			root := NewRootCommand(Dependencies{Environment: getenv, Home: home, Update: newMockUpdater(t)})
			var stderr bytes.Buffer
			root.SetErr(&stderr)

			code := run(context.Background(), root, []string{command, "gorgon"})

			assert.Equal(t, 1, code)
			assert.Contains(t, stderr.String(), "unknown command")
		})
	}
}
