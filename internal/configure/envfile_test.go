package configure

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pablovarela/media-server-engine/internal/installation"
)

const gorgonEnv = `# gorgon
INSTALLATION_NAME=gorgon
TZ=Europe/London
CONFIG_LOCATION=/home/pablo/gorgon/config

export JELLYFIN_ADMIN_USER=pablo
RESTIC_REPOSITORY=~/backups  # local for now
SONARR_URL="http://$HOST:8989"
HOMEPAGE_ALLOWED_HOSTS='a.example, b.example'
`

func TestReadEnvKeepsValuesAsWritten(t *testing.T) {
	assert.Equal(t, map[string]string{
		"INSTALLATION_NAME":      "gorgon",
		"TZ":                     "Europe/London",
		"CONFIG_LOCATION":        "/home/pablo/gorgon/config",
		"JELLYFIN_ADMIN_USER":    "pablo",
		"RESTIC_REPOSITORY":      "~/backups",
		"SONARR_URL":             "http://$HOST:8989",
		"HOMEPAGE_ALLOWED_HOSTS": "a.example, b.example",
	}, ReadEnv(gorgonEnv))
}

func TestRewriteEnvChangesOnlyTheUpdatedLines(t *testing.T) {
	rewritten := RewriteEnv(gorgonEnv, []Update{{"TZ", "Europe/Madrid"}, {"JELLYFIN_ADMIN_USER", "admin"}, {"MEDIA_SERVER_HOST", "media.example"}}, true)

	assert.Equal(t, `# gorgon
INSTALLATION_NAME=gorgon
TZ=Europe/Madrid
CONFIG_LOCATION=/home/pablo/gorgon/config

export JELLYFIN_ADMIN_USER=admin
RESTIC_REPOSITORY=~/backups  # local for now
SONARR_URL="http://$HOST:8989"
HOMEPAGE_ALLOWED_HOSTS='a.example, b.example'
MEDIA_SERVER_HOST=media.example
`, rewritten)
}

func TestRewriteEnvQuotesPlainValuesSoTheyReadBackAsEntered(t *testing.T) {
	for _, value := range []string{"a.example, b.example", "x#y", `say "hi"`, "plain"} {
		t.Run(value, func(t *testing.T) {
			rewritten := RewriteEnv("TZ=Europe/London\n", []Update{{"HOMEPAGE_ALLOWED_HOSTS", value}}, true)

			assert.Equal(t, value, installation.ParseEnv(rewritten, func(string) string { return "" })["HOMEPAGE_ALLOWED_HOSTS"])
			assert.Equal(t, value, ReadEnv(rewritten)["HOMEPAGE_ALLOWED_HOSTS"])
		})
	}
}

func TestRewriteEnvLeavesSecretValuesUnquoted(t *testing.T) {
	assert.Equal(t, "A=1\nOPENVPN_PASSWORD=p a$s#\n", RewriteEnv("A=1", []Update{{"OPENVPN_PASSWORD", "p a$s#"}}, false))
}

func TestRewriteEnvOnAnEmptyFile(t *testing.T) {
	assert.Equal(t, "TZ=UTC\n", RewriteEnv("", []Update{{"TZ", "UTC"}}, true))
}

func TestRewriteEnvWritesADollarAsWrittenSoItStillExpands(t *testing.T) {
	rewritten := RewriteEnv("RESTIC_REPOSITORY=$HOME/backups\n", []Update{{"RESTIC_REPOSITORY", "$HOME/backups2"}}, true)

	assert.Equal(t, "RESTIC_REPOSITORY=$HOME/backups2\n", rewritten)
	assert.Equal(t, "/home/me/backups2", installation.ParseEnv(rewritten, func(string) string { return "/home/me" })["RESTIC_REPOSITORY"])
}

func TestRewriteEnvQuotesASpacedValueWithADollarSoItStillExpands(t *testing.T) {
	rewritten := RewriteEnv("", []Update{{"RESTIC_REPOSITORY", "$HOME/my backups"}}, true)

	assert.Equal(t, "RESTIC_REPOSITORY=\"$HOME/my backups\"\n", rewritten)
	assert.Equal(t, "/home/me/my backups", installation.ParseEnv(rewritten, func(string) string { return "/home/me" })["RESTIC_REPOSITORY"])
}

func TestRewriteEnvChangesEveryLineOfARepeatedKey(t *testing.T) {
	rewritten := RewriteEnv("TZ=Europe/London\nA=1\nTZ=Europe/Paris\n", []Update{{"TZ", "Europe/Madrid"}}, true)

	assert.Equal(t, "TZ=Europe/Madrid\nA=1\nTZ=Europe/Madrid\n", rewritten)
	assert.Equal(t, "Europe/Madrid", ReadEnv(rewritten)["TZ"])
}

func TestRewriteEnvKeepsAnInlineComment(t *testing.T) {
	assert.Equal(t, "TZ=Europe/Madrid  # home\n", RewriteEnv("TZ=Europe/London  # home\n", []Update{{"TZ", "Europe/Madrid"}}, true))
}

func TestRewriteEnvTreatsAHashInASecretAsPartOfTheValue(t *testing.T) {
	assert.Equal(t, "OPENVPN_PASSWORD=newpass\n", RewriteEnv("OPENVPN_PASSWORD=abc #9x\n", []Update{{"OPENVPN_PASSWORD", "newpass"}}, false))
}
