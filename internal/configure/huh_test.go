package configure

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMenuLabelsLineUp(t *testing.T) {
	assert.Equal(t, "General       TZ, Jellyfin user", menuLabel(MenuItem{Title: "General", Summary: "TZ, Jellyfin user"}))
	assert.Equal(t, "Save and quit", menuLabel(MenuItem{Title: "Save and quit"}))
}

func TestEscQuitsLikeCtrlC(t *testing.T) {
	assert.ElementsMatch(t, []string{"ctrl+c", "esc"}, keys().Quit.Keys())
}

func TestOnlyPlainAnswersAreTrimmed(t *testing.T) {
	assert.Equal(t, "Europe/Madrid", answered(Field{Key: "TZ"}, " Europe/Madrid "))
	assert.Equal(t, " pass with spaces ", answered(Field{Key: "OPENVPN_PASSWORD", Masked: true}, " pass with spaces "))
}

func TestHowAMaskedFieldIsShown(t *testing.T) {
	ping := Field{Key: "HEALTHCHECKS_PING_KEY", Masked: true, Optional: true}
	password := Field{Key: "RESTIC_PASSWORD", Masked: true}
	stored := Form{Values: map[string]string{"HEALTHCHECKS_PING_KEY": "", "RESTIC_PASSWORD": ""}, Stored: map[string]bool{"HEALTHCHECKS_PING_KEY": true, "RESTIC_PASSWORD": true}}

	value, placeholder := shown(ping, stored)
	assert.Equal(t, "", value)
	assert.Equal(t, "unchanged (Enter keeps it, - removes it)", placeholder)

	_, placeholder = shown(password, stored)
	assert.Equal(t, "unchanged (Enter keeps it)", placeholder)

	typed := Form{Values: map[string]string{"RESTIC_PASSWORD": "n3w-pass"}, Stored: map[string]bool{"RESTIC_PASSWORD": true}}
	value, _ = shown(password, typed)
	assert.Equal(t, "n3w-pass", value)

	_, placeholder = shown(password, Form{Values: map[string]string{}, Stored: map[string]bool{}})
	assert.Empty(t, placeholder)
}
