package homepage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

type checksAnswer struct {
	slugs map[string]bool
	err   error
}

func (c checksAnswer) Slugs(context.Context, string) (map[string]bool, error) { return c.slugs, c.err }

const servicesWithChecks = `- Media:
    - Jellyfin:
        href: http://gorgon.local:8096
- Healthchecks:
    - Backup:
        widget:
          type: healthchecks
          url: https://healthchecks.io/api/v3/checks/?slug=@HEALTHCHECK_BACKUP@
    - Both:
        description: "@HEALTHCHECK_BACKUP@ @HEALTHCHECK_UPDATE@"
`

func TestRenderServices(t *testing.T) {
	slugFor := func(job string) string {
		return "gorgon-" + map[string]string{"BACKUP": "backup", "UPDATE": "update"}[job]
	}
	type Given struct {
		text   string
		key    string
		checks checksAnswer
	}
	type Then struct {
		yaml string
		warn string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"no key drops every Healthchecks entry": {
			Given: Given{text: servicesWithChecks},
			Then:  Then{yaml: "- Media:\n    - Jellyfin:\n        href: http://gorgon.local:8096\n"},
		},
		"existing checks fill the markers": {
			Given: Given{text: servicesWithChecks, key: "k", checks: checksAnswer{slugs: map[string]bool{"gorgon-backup": true, "gorgon-update": true}}},
			Then: Then{yaml: "- Media:\n    - Jellyfin:\n        href: http://gorgon.local:8096\n- Healthchecks:\n    - Backup:\n        widget:\n          type: healthchecks\n          url: https://healthchecks.io/api/v3/checks/?slug=gorgon-backup\n" +
				"    - Both:\n        description: \"gorgon-backup gorgon-update\"\n"},
		},
		"one of two checks missing drops that tile": {
			Given: Given{text: servicesWithChecks, key: "k", checks: checksAnswer{slugs: map[string]bool{"gorgon-backup": true}}},
			Then:  Then{yaml: "- Media:\n    - Jellyfin:\n        href: http://gorgon.local:8096\n- Healthchecks:\n    - Backup:\n        widget:\n          type: healthchecks\n          url: https://healthchecks.io/api/v3/checks/?slug=gorgon-backup\n"},
		},
		"a group left empty is dropped": {
			Given: Given{text: servicesWithChecks, key: "k", checks: checksAnswer{slugs: map[string]bool{}}},
			Then:  Then{yaml: "- Media:\n    - Jellyfin:\n        href: http://gorgon.local:8096\n"},
		},
		"healthchecks unreachable": {
			Given: Given{text: servicesWithChecks, key: "k", checks: checksAnswer{err: errors.New("timeout")}},
			Then: Then{
				yaml: "- Media:\n    - Jellyfin:\n        href: http://gorgon.local:8096\n",
				warn: "could not read the checks from healthchecks (timeout); the page is drawn without them\n",
			},
		},
		"no markers needs no call": {
			Given: Given{text: "- Media:\n    - Jellyfin:\n        href: x\n", key: "k", checks: checksAnswer{err: errors.New("must not be called")}},
			Then:  Then{yaml: "- Media:\n    - Jellyfin:\n        href: x\n"},
		},
		"empty file": {
			Given: Given{text: "# nothing yet\n"},
			Then:  Then{yaml: "[]\n"},
		},
		"empty document": {
			Given: Given{text: "---\n# nothing yet\n"},
			Then:  Then{yaml: "[]\n"},
		},
		"null document": {
			Given: Given{text: "~\n"},
			Then:  Then{yaml: "[]\n"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var warn bytes.Buffer

			rendered, err := renderServices(context.Background(), tt.Given.text, slugFor, tt.Given.key, tt.Given.checks, &warn)

			require.NoError(t, err)
			assert.Equal(t, tt.Then.yaml, rendered)
			assert.Equal(t, tt.Then.warn, warn.String())
		})
	}
}

func TestRenderWidgets(t *testing.T) {
	text := "- greeting:\n    text: hi\n    href: \"\"\n    target: _blank\n- datetime:\n    href: https://example.com\n    target: _blank\n"

	rendered, err := renderWidgets(text)

	require.NoError(t, err)
	assert.Equal(t, "- greeting:\n    text: hi\n- datetime:\n    href: https://example.com\n    target: _blank\n", rendered)
}

const statusGroup = `- Status:
    - Healthchecks:
        icon: mdi-heart-pulse-#e5484d
        href: https://healthchecks.io/
        widgets:
          - type: customapi
            url: https://healthchecks.io/api/v3/checks/?slug=@HEALTHCHECK_BACKUP@
          - type: customapi
            url: https://healthchecks.io/api/v3/checks/?slug=@HEALTHCHECK_UPDATE@
    - VPN:
        href: http://gorgon.local:8000
`

func TestTheHealthchecksTileInTheStatusGroup(t *testing.T) {
	slugFor := func(job string) string { return "gorgon-" + strings.ToLower(job) }
	vpnOnly := "- Status:\n    - VPN:\n        href: http://gorgon.local:8000\n"
	type Given struct {
		key    string
		checks checksAnswer
	}
	tests := map[string]struct {
		Given Given
		Then  struct{ yaml string }
	}{
		"every check exists": {
			Given: Given{key: "k", checks: checksAnswer{slugs: map[string]bool{"gorgon-backup": true, "gorgon-update": true}}},
			Then: struct{ yaml string }{"- Status:\n    - Healthchecks:\n        icon: mdi-heart-pulse-#e5484d\n        href: https://healthchecks.io/\n        widgets:\n" +
				"          - type: customapi\n            url: https://healthchecks.io/api/v3/checks/?slug=gorgon-backup\n" +
				"          - type: customapi\n            url: https://healthchecks.io/api/v3/checks/?slug=gorgon-update\n" +
				"    - VPN:\n        href: http://gorgon.local:8000\n"},
		},
		"a missing check drops only its row": {
			Given: Given{key: "k", checks: checksAnswer{slugs: map[string]bool{"gorgon-backup": true}}},
			Then: struct{ yaml string }{"- Status:\n    - Healthchecks:\n        icon: mdi-heart-pulse-#e5484d\n        href: https://healthchecks.io/\n        widgets:\n" +
				"          - type: customapi\n            url: https://healthchecks.io/api/v3/checks/?slug=gorgon-backup\n" +
				"    - VPN:\n        href: http://gorgon.local:8000\n"},
		},
		"no check at all drops the tile and keeps the group": {
			Given: Given{key: "k", checks: checksAnswer{slugs: map[string]bool{}}},
			Then:  struct{ yaml string }{vpnOnly},
		},
		"no key drops the tile and keeps the group": {
			Then: struct{ yaml string }{vpnOnly},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rendered, err := renderServices(context.Background(), statusGroup, slugFor, tt.Given.key, tt.Given.checks, &bytes.Buffer{})

			require.NoError(t, err)
			assert.Equal(t, tt.Then.yaml, rendered)
		})
	}
}

func TestTheDefaultPageLeadsItsStatusGroupWithTheHealthchecksTile(t *testing.T) {
	text, err := os.ReadFile("../../homepage/services.yaml")
	require.NoError(t, err)
	checks := checksAnswer{slugs: map[string]bool{"gorgon-backup": true, "gorgon-update": true, "gorgon-verify": true}}

	rendered, err := renderServices(context.Background(), string(text), func(job string) string { return "gorgon-" + strings.ToLower(job) }, "k", checks, &bytes.Buffer{})

	require.NoError(t, err)
	var groups []map[string][]map[string]struct {
		Href    string `yaml:"href"`
		Widgets []struct {
			URL string `yaml:"url"`
		} `yaml:"widgets"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(rendered), &groups))
	var status []map[string]struct {
		Href    string `yaml:"href"`
		Widgets []struct {
			URL string `yaml:"url"`
		} `yaml:"widgets"`
	}
	for _, group := range groups {
		if tiles, found := group["Status"]; found {
			status = tiles
		}
	}
	require.NotEmpty(t, status)
	tile, found := status[0]["Healthchecks"]
	require.True(t, found, "the Status group starts with the Healthchecks tile")
	assert.Equal(t, "https://healthchecks.io/", tile.Href)
	var slugs []string
	for _, widget := range tile.Widgets {
		slugs = append(slugs, strings.SplitN(widget.URL, "slug=", 2)[1])
	}
	assert.Equal(t, []string{"gorgon-backup", "gorgon-update", "gorgon-verify"}, slugs)
}
