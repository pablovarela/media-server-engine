package github

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const bearer = "Bearer test-token"

func TestLogin(t *testing.T) {
	client := clientAnswering(t, exchange{url: "https://api.github.com/user", authorization: bearer, status: http.StatusOK, body: `{"login": "pablovarela"}`})

	login, err := client.Login(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "pablovarela", login)
}

func TestRepositoryExists(t *testing.T) {
	url := "https://api.github.com/repos/pablovarela/media-server-config-gorgon"
	tests := map[string]struct {
		status int
		exists bool
		err    string
	}{
		"there":       {status: http.StatusOK, exists: true},
		"not there":   {status: http.StatusNotFound},
		"broken":      {status: http.StatusInternalServerError, err: "look up pablovarela/media-server-config-gorgon: GitHub answered 500 Internal Server Error"},
		"not allowed": {status: http.StatusUnauthorized, err: "look up pablovarela/media-server-config-gorgon: GitHub answered 401 Unauthorized: check the token from GITHUB_TOKEN (gh auth login)"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := clientAnswering(t, exchange{url: url, authorization: bearer, status: tt.status, body: `{}`})

			exists, err := client.RepositoryExists(context.Background(), "pablovarela", "media-server-config-gorgon")

			if tt.err != "" {
				assert.EqualError(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.exists, exists)
		})
	}
}

func TestCreatePrivateRepository(t *testing.T) {
	tests := map[string]struct{ org, url string }{
		"under the user":        {url: "https://api.github.com/user/repos"},
		"under an organisation": {org: "family", url: "https://api.github.com/orgs/family/repos"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := clientAnswering(t, exchange{
				method: http.MethodPost, url: tt.url, authorization: bearer,
				request: `{"name": "media-server-config-gorgon", "private": true}`,
				status:  http.StatusCreated, body: `{"clone_url": "https://github.com/x/media-server-config-gorgon.git"}`,
			})

			cloneURL, err := client.CreatePrivateRepository(context.Background(), tt.org, "media-server-config-gorgon")

			require.NoError(t, err)
			assert.Equal(t, "https://github.com/x/media-server-config-gorgon.git", cloneURL)
		})
	}
}

func TestCreatePrivateRepositoryAlreadyTaken(t *testing.T) {
	client := clientAnswering(t, exchange{
		method: http.MethodPost, url: "https://api.github.com/user/repos", authorization: bearer,
		request: `{"name": "media-server-config-gorgon", "private": true}`,
		status:  http.StatusUnprocessableEntity, body: `{"message": "Repository creation failed.", "errors": [{"resource": "Repository", "code": "custom", "field": "name", "message": "name already exists on this account"}]}`,
	})

	_, err := client.CreatePrivateRepository(context.Background(), "", "media-server-config-gorgon")

	assert.ErrorIs(t, err, ErrRepositoryTaken)
	assert.EqualError(t, err, "create media-server-config-gorgon: the repository already exists")
}
