package cmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogins(t *testing.T) {
	getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\nJELLYFIN_ADMIN_USER=pablo\n"})
	decrypt := func(path string) ([]byte, error) {
		return []byte("JELLYFIN_ADMIN_PASSWORD=jf\nDELUGE_WEB_PASSWORD=dl\nPORTAINER_ADMIN_PASSWORD=pt\n"), nil
	}
	root := NewRootCommand(Dependencies{Environment: getenv, Home: home, Decrypt: decrypt, Update: newMockUpdater(t)})
	var stdout bytes.Buffer
	root.SetOut(&stdout)

	code := run(context.Background(), root, []string{"logins"})

	assert.Equal(t, 0, code)
	assert.Equal(t, "App          User       Password\n"+
		"Jellyfin     pablo      jf\n"+
		"Seerr        sign in with the Jellyfin account\n"+
		"Deluge                  dl\n"+
		"Portainer    admin      pt\n"+
		"Sonarr       no login on the local network\n"+
		"Radarr       no login on the local network\n"+
		"Prowlarr     no login on the local network\n", stdout.String())
}
