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

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/installation"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReload(t *testing.T) {
	project := &types.Project{Name: "media-server"}
	running := []compose.Container{{Name: "homepage", State: "running"}}
	type Given struct {
		containers []compose.Container
		psErr      error
		changes    pageChanges
	}
	type Then struct {
		expect      func(r *mockComposeRunner)
		revalidated bool
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"docker unreachable": {Given: Given{psErr: errors.New("cannot connect"), changes: pageChanges{env: true}}},
		"not running":        {Given: Given{containers: []compose.Container{{Name: "homepage", State: "exited"}}, changes: pageChanges{env: true}}},
		"env changed recreates it": {
			Given: Given{containers: running, changes: pageChanges{env: true, images: true}},
			Then:  Then{expect: func(r *mockComposeRunner) { r.EXPECT().Up(mock.Anything, project, []string{"homepage"}).Return(nil) }},
		},
		"images changed restarts it": {
			Given: Given{containers: running, changes: pageChanges{images: true}},
			Then: Then{expect: func(r *mockComposeRunner) {
				r.EXPECT().Restart(mock.Anything, project, []string{"homepage"}).Return(nil)
			}},
		},
		"otherwise it revalidates": {
			Given: Given{containers: running},
			Then:  Then{revalidated: true},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			runner := newMockComposeRunner(t)
			runner.EXPECT().Ps(mock.Anything, project).Return(tt.Given.containers, tt.Given.psErr)
			if tt.Then.expect != nil {
				tt.Then.expect(runner)
			}
			revalidated := false
			deps := Dependencies{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				assert.Equal(t, "http://localhost:8080/api/revalidate", r.URL.String())
				revalidated = true
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
			})}}

			require.NoError(t, reload(context.Background(), deps, runner, project, "8080", tt.Given.changes))

			assert.Equal(t, tt.Then.revalidated, revalidated)
		})
	}
}

func TestHomepageCommand(t *testing.T) {
	project := &types.Project{Name: "media-server"}
	getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
	state := filepath.Join(home, ".local", "state", "mse", "gorgon")
	runner := newMockComposeRunner(t)
	runner.EXPECT().Load(mock.Anything, mock.Anything, compose.Stack, mock.Anything, mock.Anything).RunAndReturn(
		func(context.Context, *installation.Installation, compose.Kind, []string, []string) (*types.Project, error) {
			env, err := os.ReadFile(filepath.Join(state, ".secrets", "homepage.env"))
			require.NoError(t, err)
			assert.Contains(t, string(env), "HOMEPAGE_VAR_SONARR_KEY=s\n", "the project loads the new homepage.env")
			return project, nil
		})
	runner.EXPECT().Ps(mock.Anything, project).Return([]compose.Container{{Name: "homepage", State: "running"}}, nil)
	runner.EXPECT().Up(mock.Anything, project, []string{"homepage"}).Return(nil)
	deps := Dependencies{
		Environment: getenv, Home: home, Update: newMockUpdater(t),
		Decrypt: func(string) ([]byte, error) { return []byte("SONARR_API_KEY=s\n"), nil },
		Host:    installation.Host{GOOS: "linux", Hostname: func() (string, error) { return "gorgon.local", nil }},
		Engine: fstest.MapFS{
			"docker-compose.yml":            {Data: []byte("services: {}\n")},
			"docker-compose.monitoring.yml": {Data: []byte("services: {}\n")},
			"grafana/datasource.yml":        {Data: []byte("# fixture\n")},
			"prometheus/prometheus.yml":     {Data: []byte("# fixture\n")},
			"homepage/settings.yaml":        {Data: []byte("title: \"@INSTALLATION_NAME@ on @HOST@\"\n")},
			"homepage/services.yaml":        {Data: []byte("[]\n")},
			"homepage/widgets.yaml":         {Data: []byte("[]\n")},
			"homepage/bookmarks.yaml":       {Data: []byte("[]\n")},
			"homepage/custom.css":           {Data: []byte("")},
		},
		Compose: func(_, _ io.Writer) (composeRunner, error) { return runner, nil },
	}
	root := NewRootCommand(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"homepage"})

	assert.Equal(t, 0, code, stderr.String())
	assert.Equal(t, "The landing page is redrawn; an open page reloads itself in a few seconds.\n", stdout.String())
	settings, err := os.ReadFile(filepath.Join(state, ".homepage", "settings.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "title: \"gorgon on gorgon.local\"\n", string(settings))
}
