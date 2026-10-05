package homepage

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
