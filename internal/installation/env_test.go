package installation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseEnv(t *testing.T) {
	type Given struct {
		text string
	}
	type Then struct {
		settings map[string]string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"plain lines as configure writes them": {
			Given: Given{text: "INSTALLATION_NAME=gorgon\nTZ=Europe/London\nRESTIC_REPOSITORY=b2:bucket:gorgon\n"},
			Then:  Then{settings: map[string]string{"INSTALLATION_NAME": "gorgon", "TZ": "Europe/London", "RESTIC_REPOSITORY": "b2:bucket:gorgon"}},
		},
		"comments, blank lines and export": {
			Given: Given{text: "# added by hand\n\nexport MEDIA_SERVER_HOST=media.example\n"},
			Then:  Then{settings: map[string]string{"MEDIA_SERVER_HOST": "media.example"}},
		},
		"surrounding quotes are removed once": {
			Given: Given{text: "A=\"two words\"\nB='single'\nC=\"'kept'\"\n"},
			Then:  Then{settings: map[string]string{"A": "two words", "B": "single", "C": "'kept'"}},
		},
		"values keep their =": {
			Given: Given{text: "A=x=y\n"},
			Then:  Then{settings: map[string]string{"A": "x=y"}},
		},
		"~ and variables expand as bash expands them": {
			Given: Given{text: "RESTIC_REPOSITORY=~/backups\nHOMEPAGE_ALLOWED_HOSTS=\"$HOME/x\"\nB=${HOME}/y\nC=$EARLIER/z\nD='$HOME'\nE=\"~/not\"\nF=a~b\nG=$UNSET\n"},
			Then: Then{settings: map[string]string{
				"RESTIC_REPOSITORY": "/home/me/backups", "HOMEPAGE_ALLOWED_HOSTS": "/home/me/x", "B": "/home/me/y",
				"C": "first/z", "D": "$HOME", "E": "~/not", "F": "a~b", "G": "",
			}},
		},
		"a comment after a value is dropped, as bash drops it": {
			Given: Given{text: "A=1 # set by hand\nB=\"x # y\"\nC=a#b\n"},
			Then:  Then{settings: map[string]string{"A": "1", "B": "x # y", "C": "a#b"}},
		},
		"lines without = are ignored": {
			Given: Given{text: "not a setting\nA=1\n"},
			Then:  Then{settings: map[string]string{"A": "1"}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			environment := map[string]string{"HOME": "/home/me", "EARLIER": "from the environment"}
			text := tt.Given.text
			if strings.Contains(text, "$EARLIER") {
				text = "EARLIER=first\n" + text
				tt.Then.settings["EARLIER"] = "first"
			}

			assert.Equal(t, tt.Then.settings, ParseEnv(text, func(key string) string { return environment[key] }))
		})
	}
}
