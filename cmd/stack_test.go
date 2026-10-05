package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/installation"
)

func TestStackCommands(t *testing.T) {
	project := &types.Project{Name: "media-server"}
	type When struct {
		args []string
	}
	type Then struct {
		expect func(r *mockComposeRunner)
		stdout string
		drawn  bool
	}
	tests := map[string]struct {
		When When
		Then Then
	}{
		"stack up draws the page first": {
			When: When{args: []string{"stack", "up"}},
			Then: Then{expect: func(r *mockComposeRunner) { r.EXPECT().Up(mock.Anything, project, []string{}).Return(nil) }, drawn: true},
		},
		"stack up with services": {
			When: When{args: []string{"stack", "up", "jellyfin"}},
			Then: Then{expect: func(r *mockComposeRunner) { r.EXPECT().Up(mock.Anything, project, []string{"jellyfin"}).Return(nil) }, drawn: true},
		},
		"stack down": {
			When: When{args: []string{"stack", "down"}},
			Then: Then{expect: func(r *mockComposeRunner) { r.EXPECT().Down(mock.Anything, project).Return(nil) }},
		},
		"stack restart": {
			When: When{args: []string{"stack", "restart", "homepage"}},
			Then: Then{expect: func(r *mockComposeRunner) {
				r.EXPECT().Restart(mock.Anything, project, []string{"homepage"}).Return(nil)
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
		"monitoring ps": {
			When: When{args: []string{"monitoring", "ps"}},
			Then: Then{expect: func(r *mockComposeRunner) { r.EXPECT().Ps(mock.Anything, project).Return(nil, nil) }, stdout: "NAME  STATE  HEALTH  PORTS\n"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
			runner := newMockComposeRunner(t)
			kind := compose.Stack
			if tt.When.args[0] == "monitoring" {
				kind = compose.Monitoring
			}
			runner.EXPECT().Load(mock.Anything, mock.Anything, kind, mock.Anything, mock.Anything).Return(project, nil)
			tt.Then.expect(runner)
			decrypt := func(string) ([]byte, error) { return []byte("A=1\n"), nil }
			deps := Dependencies{
				Environment: getenv, Home: home, Decrypt: decrypt, Update: newMockUpdater(t),
				Host: installation.Host{GOOS: "linux", Hostname: func() (string, error) { return "gorgon", nil }},
				Engine: fstest.MapFS{
					"docker-compose.yml":            {Data: []byte("services: {}\n")},
					"docker-compose.monitoring.yml": {Data: []byte("services: {}\n")},
					"grafana/datasource.yml":        {Data: []byte("# fixture\n")},
					"prometheus/prometheus.yml":     {Data: []byte("# fixture\n")},
					"homepage/settings.yaml":        {Data: []byte("title: x\n")},
					"homepage/services.yaml":        {Data: []byte("[]\n")},
					"homepage/widgets.yaml":         {Data: []byte("[]\n")},
					"homepage/bookmarks.yaml":       {Data: []byte("[]\n")},
					"homepage/custom.css":           {Data: []byte("")},
				},
				HTTP:    &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("no network in tests") })},
				Compose: func(_, _ io.Writer) (composeRunner, error) { return runner, nil },
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
			drawnPage := filepath.Join(state, ".homepage", "settings.yaml")
			if tt.Then.drawn {
				assert.FileExists(t, drawnPage)
			} else {
				assert.NoFileExists(t, drawnPage)
			}
		})
	}
}
