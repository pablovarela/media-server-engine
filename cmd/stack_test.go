package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/paint"
)

func TestStackCommands(t *testing.T) {
	project := &types.Project{Name: "media-server"}
	homepageStopped := func(r *mockComposeRunner) { r.EXPECT().Ps(mock.Anything, project).Return(nil, nil) }
	type Given struct {
		configServices string
	}
	type When struct {
		args []string
	}
	type Then struct {
		expect     func(r *mockComposeRunner)
		stdout     string
		stderr     string
		drawn      bool
		envWritten bool
	}
	tests := map[string]struct {
		Given Given
		When  When
		Then  Then
	}{
		"stack up draws the page first": {
			When: When{args: []string{"stack", "up"}},
			Then: Then{stdout: "Drawing the landing page... done.\nStarting the stack... done.\nReloading Homepage... not running.\n", expect: func(r *mockComposeRunner) {
				r.EXPECT().Up(mock.Anything, project, []string{}, compose.NoWait).Return(nil)
				homepageStopped(r)
			}, drawn: true},
		},
		"stack up starts the containers when the page cannot be drawn": {
			Given: Given{configServices: "not: a list\n"},
			When:  When{args: []string{"stack", "up"}},
			Then: Then{stdout: "Drawing the landing page... failed.\nStarting the stack... done.\n",
				expect: func(r *mockComposeRunner) {
					r.EXPECT().Up(mock.Anything, project, []string{}, compose.NoWait).Return(nil)
				},
				stderr:     "could not draw the landing page (services.yaml: expected a list); it keeps its previous files\n",
				drawn:      true,
				envWritten: true,
			},
		},
		"stack up --wait": {
			When: When{args: []string{"stack", "up", "--wait"}},
			Then: Then{stdout: "Drawing the landing page... done.\nStarting the stack... done.\nReloading Homepage... not running.\n", expect: func(r *mockComposeRunner) {
				r.EXPECT().Up(mock.Anything, project, []string{}, compose.Wait{Enabled: true, Timeout: 5 * time.Minute}).Return(nil)
				homepageStopped(r)
			}, drawn: true},
		},
		"stack up --wait-timeout": {
			When: When{args: []string{"stack", "up", "--wait", "--wait-timeout", "2m"}},
			Then: Then{stdout: "Drawing the landing page... done.\nStarting the stack... done.\nReloading Homepage... not running.\n", expect: func(r *mockComposeRunner) {
				r.EXPECT().Up(mock.Anything, project, []string{}, compose.Wait{Enabled: true, Timeout: 2 * time.Minute}).Return(nil)
				homepageStopped(r)
			}, drawn: true},
		},
		"stack up with services": {
			When: When{args: []string{"stack", "up", "jellyfin"}},
			Then: Then{stdout: "Drawing the landing page... done.\nStarting jellyfin... done.\nReloading Homepage... not running.\n", expect: func(r *mockComposeRunner) {
				r.EXPECT().Up(mock.Anything, project, []string{"jellyfin"}, compose.NoWait).Return(nil)
				homepageStopped(r)
			}, drawn: true},
		},
		"stack down": {
			When: When{args: []string{"stack", "down"}},
			Then: Then{stdout: "Stopping the stack... done.\n", expect: func(r *mockComposeRunner) { r.EXPECT().Down(mock.Anything, project).Return(nil) }},
		},
		"stack restart": {
			When: When{args: []string{"stack", "restart", "homepage"}},
			Then: Then{stdout: "Drawing the landing page... done.\nRestarting homepage... done.\nReloading Homepage... not running.\n", expect: func(r *mockComposeRunner) {
				r.EXPECT().Restart(mock.Anything, project, []string{"homepage"}).Return(nil)
				homepageStopped(r)
			}, drawn: true},
		},
		"stack restart recreates homepage when its environment changed": {
			When: When{args: []string{"stack", "restart"}},
			Then: Then{stdout: "Drawing the landing page... done.\nRestarting the stack... done.\nReloading Homepage... recreated.\n", expect: func(r *mockComposeRunner) {
				r.EXPECT().Restart(mock.Anything, project, []string{}).Return(nil)
				r.EXPECT().Ps(mock.Anything, project).Return([]compose.Container{{Name: "homepage", State: "running"}}, nil)
				r.EXPECT().Up(mock.Anything, project, []string{"homepage"}, compose.NoWait).Return(nil)
			}, drawn: true},
		},
		"stack ps": {
			When: When{args: []string{"stack", "ps"}},
			Then: Then{
				expect: func(r *mockComposeRunner) {
					r.EXPECT().Ps(mock.Anything, project).Return([]compose.Container{
						{Name: "jellyfin", State: "running", Health: "healthy", Ports: []string{"8096->8096/tcp"}},
						{Name: "configarr", State: "exited"},
					}, nil)
				},
				stdout: "NAME       STATE    HEALTH   PORTS\njellyfin   running  healthy  8096->8096/tcp\nconfigarr  exited\n",
			},
		},
		"stack ps keeps columns aligned around empty cells": {
			When: When{args: []string{"stack", "ps"}},
			Then: Then{
				expect: func(r *mockComposeRunner) {
					r.EXPECT().Ps(mock.Anything, project).Return([]compose.Container{
						{Name: "bazarr", State: "running", Ports: []string{"6767->6767/tcp"}},
						{Name: "deluge", State: "running"},
						{Name: "gluetun", State: "running", Health: "healthy", Ports: []string{"8112->8112/tcp"}},
					}, nil)
				},
				stdout: "NAME     STATE    HEALTH   PORTS\nbazarr   running           6767->6767/tcp\ndeluge   running\ngluetun  running  healthy  8112->8112/tcp\n",
			},
		},
		"stack logs": {
			When: When{args: []string{"stack", "logs", "-f", "--tail", "20", "jellyfin"}},
			Then: Then{expect: func(r *mockComposeRunner) {
				r.EXPECT().Logs(mock.Anything, project, compose.LogsOptions{Follow: true, Tail: "20", Services: []string{"jellyfin"}}, mock.Anything).Return(nil)
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
			if tt.Given.configServices != "" {
				services := filepath.Join(home, ".config", "mse", "gorgon", "homepage", "services.yaml")
				require.NoError(t, os.MkdirAll(filepath.Dir(services), 0o755))
				require.NoError(t, os.WriteFile(services, []byte(tt.Given.configServices), 0o644))
			}
			runner := newMockComposeRunner(t)
			runner.EXPECT().Load(mock.Anything, mock.Anything, compose.Stack, mock.Anything, mock.Anything).Return(project, nil)
			tt.Then.expect(runner)
			decrypt := func(string) ([]byte, error) { return []byte("A=1\n"), nil }
			deps := Dependencies{
				Environment: getenv, Home: home, Decrypt: decrypt, Update: newMockUpdater(t),
				Host: installation.Host{GOOS: "linux", Hostname: func() (string, error) { return "gorgon", nil }},
				Engine: fstest.MapFS{
					"docker-compose.yml":      {Data: []byte("services: {}\n")},
					"homepage/settings.yaml":  {Data: []byte("title: x\n")},
					"homepage/services.yaml":  {Data: []byte("[]\n")},
					"homepage/widgets.yaml":   {Data: []byte("[]\n")},
					"homepage/bookmarks.yaml": {Data: []byte("[]\n")},
					"homepage/custom.css":     {Data: []byte("")},
				},
				HTTP:    &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("no network in tests") })},
				Compose: func(io.Writer, *compose.Outcomes) (composeRunner, error) { return runner, nil },
			}
			root := NewRootCommand(deps)
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			code := run(context.Background(), root, tt.When.args)

			assert.Equal(t, 0, code, stderr.String())
			state := filepath.Join(home, ".local", "state", "mse", "gorgon")
			assert.FileExists(t, filepath.Join(state, ".secrets", "vpn.env"))
			assert.FileExists(t, filepath.Join(state, "docker-compose.yml"))
			assert.Equal(t, tt.Then.stdout, stdout.String())
			assert.Equal(t, tt.Then.stderr, stderr.String())
			if tt.Then.envWritten {
				env, err := os.ReadFile(filepath.Join(state, ".secrets", "homepage.env"))
				require.NoError(t, err)
				assert.Contains(t, string(env), "HOMEPAGE_VAR_SONARR_KEY=")
			}
			drawnPage := filepath.Join(state, ".homepage", "settings.yaml")
			if tt.Then.drawn {
				assert.FileExists(t, drawnPage)
			} else {
				assert.NoFileExists(t, drawnPage)
			}
		})
	}
}

func TestPrintContainersInColourKeepsColumnsAligned(t *testing.T) {
	paint.Enable(true)
	t.Cleanup(func() { paint.Enable(false) })
	var out bytes.Buffer

	require.NoError(t, printContainers(&out, []compose.Container{
		{Name: "bazarr", State: "running", Ports: []string{"6767->6767/tcp"}},
		{Name: "gluetun", State: "running", Health: "unhealthy"},
		{Name: "configarr", State: "exited"},
	}))

	green, red, reset := "\x1b[32m", "\x1b[31m", "\x1b[0m"
	assert.Equal(t, "NAME       STATE    HEALTH     PORTS\n"+
		"bazarr     "+green+"running"+reset+"             6767->6767/tcp\n"+
		"gluetun    "+green+"running"+reset+"  "+red+"unhealthy"+reset+"\n"+
		"configarr  "+red+"exited"+reset+"\n", out.String())
}

func TestContainerLogsStayOutOfTheLog(t *testing.T) {
	project := &types.Project{Name: "media-server"}
	getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
	runner := newMockComposeRunner(t)
	runner.EXPECT().Load(mock.Anything, mock.Anything, compose.Stack, mock.Anything, mock.Anything).Return(project, nil)
	runner.EXPECT().Logs(mock.Anything, project, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, _ *types.Project, _ compose.LogsOptions, w io.Writer) error {
		_, err := io.WriteString(w, "jellyfin | a container log line\n")
		return err
	})
	root := NewRootCommand(Dependencies{
		Environment: getenv, Home: home, Update: newMockUpdater(t),
		Decrypt: func(string) ([]byte, error) { return []byte("A=1\n"), nil },
		Host:    installation.Host{GOOS: "linux", Hostname: func() (string, error) { return "gorgon", nil }},
		Engine: fstest.MapFS{
			"docker-compose.yml": {Data: []byte("services: {}\n")},
		},
		Compose: func(io.Writer, *compose.Outcomes) (composeRunner, error) { return runner, nil },
	})
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&bytes.Buffer{})

	code := run(context.Background(), root, []string{"stack", "logs"})

	assert.Equal(t, 0, code)
	assert.Equal(t, "jellyfin | a container log line\n", stdout.String())
	logged, err := os.ReadFile(filepath.Join(home, ".local", "state", "mse", "gorgon", "logs", "mse.log"))
	require.NoError(t, err)
	assert.NotContains(t, string(logged), "a container log line")
}

func TestComposeEventsReachTheLogAndOnlyVerboseScreens(t *testing.T) {
	project := &types.Project{Name: "media-server"}
	for _, verbose := range []bool{false, true} {
		getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
		runner := newMockComposeRunner(t)
		var tool io.Writer
		runner.EXPECT().Load(mock.Anything, mock.Anything, compose.Stack, mock.Anything, mock.Anything).Return(project, nil)
		runner.EXPECT().Down(mock.Anything, project).RunAndReturn(func(context.Context, *types.Project) error {
			_, err := io.WriteString(tool, "Container seerr Stopped\n")
			return err
		})
		root := NewRootCommand(Dependencies{
			Environment: getenv, Home: home, Update: newMockUpdater(t),
			Decrypt: func(string) ([]byte, error) { return []byte("A=1\n"), nil },
			Host:    installation.Host{GOOS: "linux", Hostname: func() (string, error) { return "gorgon", nil }},
			Engine: fstest.MapFS{
				"docker-compose.yml": {Data: []byte("services: {}\n")},
			},
			Compose: func(w io.Writer, _ *compose.Outcomes) (composeRunner, error) {
				tool = w
				return runner, nil
			},
		})
		var stdout bytes.Buffer
		root.SetOut(&stdout)
		root.SetErr(&bytes.Buffer{})
		args := []string{"stack", "down"}
		if verbose {
			args = append(args, "-v")
		}

		code := run(context.Background(), root, args)

		assert.Equal(t, 0, code)
		logged, err := os.ReadFile(filepath.Join(home, ".local", "state", "mse", "gorgon", "logs", "mse.log"))
		require.NoError(t, err)
		assert.Contains(t, string(logged), "compose | Container seerr Stopped")
		assert.Equal(t, verbose, strings.Contains(stdout.String(), "compose | Container seerr Stopped"), "verbose %v", verbose)
	}
}

func TestAnUnwritableLogDoesNotStopACommand(t *testing.T) {
	getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
	state := filepath.Join(home, ".local", "state", "mse", "gorgon")
	require.NoError(t, os.MkdirAll(state, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(state, "logs"), nil, 0o644))
	root := NewRootCommand(Dependencies{Environment: getenv, Home: home, Update: newMockUpdater(t), Host: installation.Host{GOOS: "linux", Hostname: func() (string, error) { return "gorgon", nil }},
		Decrypt: func(string) ([]byte, error) { return []byte("SONARR_API_KEY=k\n"), nil }})
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"logins"})

	assert.Equal(t, 0, code)
	assert.Contains(t, stdout.String(), "Jellyfin")
	assert.Equal(t, 1, strings.Count(stderr.String(), "could not write the log "))
}
