package cmd

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/compose"
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
		result      string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"docker unreachable": {Given: Given{psErr: errors.New("cannot connect"), changes: pageChanges{env: true}}, Then: Then{result: "not running"}},
		"not running":        {Given: Given{containers: []compose.Container{{Name: "homepage", State: "exited"}}, changes: pageChanges{env: true}}, Then: Then{result: "not running"}},
		"env changed recreates it": {
			Given: Given{containers: running, changes: pageChanges{env: true, images: true}},
			Then: Then{expect: func(r *mockComposeRunner) {
				r.EXPECT().Up(mock.Anything, project, []string{"homepage"}, compose.NoWait).Return(nil)
			}, result: "recreated"},
		},
		"images changed restarts it": {
			Given: Given{containers: running, changes: pageChanges{images: true}},
			Then: Then{expect: func(r *mockComposeRunner) {
				r.EXPECT().Restart(mock.Anything, project, []string{"homepage"}).Return(nil)
			}, result: "restarted"},
		},
		"otherwise it revalidates": {
			Given: Given{containers: running},
			Then:  Then{revalidated: true, result: "asked it to revalidate"},
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

			result, err := reload(context.Background(), deps, runner, project, "8080", tt.Given.changes)

			require.NoError(t, err)
			assert.Equal(t, tt.Then.result, result)
			assert.Equal(t, tt.Then.revalidated, revalidated)
		})
	}
}
