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
