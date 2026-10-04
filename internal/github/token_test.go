package github

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToken(t *testing.T) {
	type Given struct {
		env     string
		gh      string
		ghError error
	}
	type Then struct {
		token string
		err   string
	}
	noToken := "set GITHUB_TOKEN to a token that can read pablovarela/media-server-engine, or log in with gh auth login"
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"from GITHUB_TOKEN": {
			Given: Given{env: "env-token", gh: "gh-token\n"},
			Then:  Then{token: "env-token"},
		},
		"from gh when GITHUB_TOKEN is empty": {
			Given: Given{gh: "gh-token\n"},
			Then:  Then{token: "gh-token"},
		},
		"gh missing or logged out": {
			Given: Given{ghError: errors.New("exec: \"gh\": executable file not found in $PATH")},
			Then:  Then{err: noToken},
		},
		"gh answering nothing": {
			Given: Given{gh: "\n"},
			Then:  Then{err: noToken},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			source := TokenSource{
				Getenv: func(key string) string {
					assert.Equal(t, "GITHUB_TOKEN", key)
					return tt.Given.env
				},
				GHToken: func(context.Context) ([]byte, error) { return []byte(tt.Given.gh), tt.Given.ghError },
			}

			token, err := source.Token(context.Background())

			assert.Equal(t, tt.Then.token, token)
			if tt.Then.err == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.Then.err)
			}
		})
	}
}
