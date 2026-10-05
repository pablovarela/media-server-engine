package configure

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func complete() Values {
	v := Values{}
	for _, section := range Sections() {
		for _, f := range section.Fields {
			v = v.With(f.File, f.Key, "current-"+f.Key)
		}
	}
	v = v.With(PlainFile, "TZ", "Europe/London")
	for _, app := range Rotatable {
		v = v.With(AppsFile, app.Key, "old-"+app.Key)
	}
	return v
}

func TestDiffAndSummary(t *testing.T) {
	before := complete()
	after := before.With(PlainFile, "TZ", "Europe/Madrid").
		With("secrets/vpn.sops.env", "OPENVPN_PASSWORD", "n3w-pass").
		With(PlainFile, "MEDIA_SERVER_HOST", "").
		With(AppsFile, "SONARR_API_KEY", "0123456789abcdef0123456789abcdef")

	changes := Diff(before, after)

	assert.Equal(t, []string{
		"General       TZ: Europe/London -> Europe/Madrid",
		"General       MEDIA_SERVER_HOST: current-MEDIA_SERVER_HOST -> (none)",
		"VPN           OPENVPN_PASSWORD changed",
		"Rotate keys   SONARR_API_KEY new",
	}, Summary(changes))
	for _, line := range Summary(changes) {
		assert.NotContains(t, line, "n3w-pass")
		assert.NotContains(t, line, "current-OPENVPN_PASSWORD")
		assert.NotContains(t, line, "0123456789abcdef")
	}
	assert.Equal(t, "Configure gorgon: General, VPN, rotate Sonarr key", CommitMessage("gorgon", changes))
}

func TestASecretSetForTheFirstTimeIsNew(t *testing.T) {
	before := Values{}
	after := before.With("secrets/healthchecks.sops.env", "HEALTHCHECKS_PING_KEY", "ping")

	assert.Equal(t, []string{"Healthchecks  HEALTHCHECKS_PING_KEY new"}, Summary(Diff(before, after)))
}

func TestNoDiffWhenNothingChanged(t *testing.T) {
	assert.Empty(t, Diff(complete(), complete()))
}

func TestMissing(t *testing.T) {
	var all []string
	for _, section := range Missing(Values{}) {
		all = append(all, section.Name)
	}
	assert.Equal(t, []string{"General", "Backups", "VPN", "Healthchecks", "App logins"}, all)

	full := complete().With(PlainFile, "HOMEPAGE_PORT", "").With("secrets/vpn.sops.env", "SERVER_COUNTRIES", "")
	assert.Empty(t, Missing(full))

	var vpn []string
	for _, section := range Missing(full.With("secrets/vpn.sops.env", "OPENVPN_USER", "")) {
		vpn = append(vpn, section.Name)
	}
	assert.Equal(t, []string{"VPN"}, vpn)
}

func TestRotate(t *testing.T) {
	rotated, err := Rotate(complete(), []App{Rotatable[0], Rotatable[2]}, func() (string, error) { return "0123456789abcdef0123456789abcdef", nil })

	require.NoError(t, err)
	assert.Equal(t, "0123456789abcdef0123456789abcdef", rotated.Get(AppsFile, "SONARR_API_KEY"))
	assert.Equal(t, "old-RADARR_API_KEY", rotated.Get(AppsFile, "RADARR_API_KEY"))
	assert.Equal(t, "0123456789abcdef0123456789abcdef", rotated.Get(AppsFile, "PROWLARR_API_KEY"))
}
