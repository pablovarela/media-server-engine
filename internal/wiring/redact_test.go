package wiring

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHideReplacesEveryKnownSecretLongestFirst(t *testing.T) {
	r := NewRedactor(map[string]string{"SONARR_API_KEY": "sonarr-key", "LONGER": "sonarr-key-2", "SHORT": "abc"})

	assert.Equal(t, "<hidden> and <hidden>, abc", r.Hide("sonarr-key and sonarr-key-2, abc"))
}

func TestHideLeavesASecretInsideAWordAlone(t *testing.T) {
	r := NewRedactor(map[string]string{"KEY": "secret1"})

	assert.Equal(t, "xsecret1x <hidden>", r.Hide("xsecret1x secret1"))
}

func TestSentRemembersHeadersAndSecretLookingFields(t *testing.T) {
	r := NewRedactor(nil)

	r.Sent(map[string]string{"Authorization": `MediaBrowser Client="media-server", Token="jellyfin-token"`}, map[string]any{
		"Username": "admin", "Pw": "not-a-secret-name", "password": "hunter22",
		"nested": []any{map[string]any{"apiKey": "nested-key"}},
	})
	r.Sent(nil, url.Values{"settings-sonarr-apikey": {"form-secret"}})

	assert.Equal(t, "admin <hidden> <hidden> <hidden> not-a-secret-name <hidden>",
		r.Hide("admin jellyfin-token hunter22 nested-key not-a-secret-name form-secret"))
}
