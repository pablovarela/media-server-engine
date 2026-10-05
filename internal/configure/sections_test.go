package configure

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func field(t *testing.T, key string) Field {
	t.Helper()
	for _, section := range Sections() {
		for _, f := range section.Fields {
			if f.Key == key {
				return f
			}
		}
	}
	t.Fatalf("no field %s", key)
	return Field{}
}

func TestTheSections(t *testing.T) {
	var names []string
	for _, section := range Sections() {
		names = append(names, section.Name)
	}
	assert.Equal(t, []string{"General", "Backups", "VPN", "Healthchecks", "App logins"}, names)
	assert.Equal(t, PlainFile, field(t, "RESTIC_REPOSITORY").File)
	assert.Equal(t, "secrets/backup.sops.env", field(t, "RESTIC_PASSWORD").File)
	assert.True(t, field(t, "OPENVPN_PASSWORD").Masked)
	assert.False(t, field(t, "VPN_SERVICE_PROVIDER").Masked)
	assert.True(t, field(t, "SERVER_COUNTRIES").Optional)
}

func TestTheChecks(t *testing.T) {
	tests := map[string]struct {
		key, value, err string
	}{
		"a time zone":           {key: "TZ", value: "Europe/Madrid"},
		"not a time zone":       {key: "TZ", value: "Europe/Madird", err: "TZ: Europe/Madird isn't a time zone"},
		"an empty time zone":    {key: "TZ", value: "", err: "TZ: can't be empty"},
		"a port":                {key: "HOMEPAGE_PORT", value: "8080"},
		"no port":               {key: "HOMEPAGE_PORT", value: ""},
		"port 0":                {key: "HOMEPAGE_PORT", value: "0", err: "HOMEPAGE_PORT: must be a number from 1 to 65535"},
		"port 70000":            {key: "HOMEPAGE_PORT", value: "70000", err: "HOMEPAGE_PORT: must be a number from 1 to 65535"},
		"a port that's a word":  {key: "HOMEPAGE_PORT", value: "abc", err: "HOMEPAGE_PORT: must be a number from 1 to 65535"},
		"no Jellyfin user":      {key: "JELLYFIN_ADMIN_USER", value: "", err: "JELLYFIN_ADMIN_USER: can't be empty"},
		"a quote in plain text": {key: "MEDIA_SERVER_HOST", value: "it's", err: "MEDIA_SERVER_HOST: can't contain '"},
		"a quote in a secret":   {key: "OPENVPN_PASSWORD", value: "it's"},
		"a newline":             {key: "OPENVPN_PASSWORD", value: "a\nb", err: "OPENVPN_PASSWORD: can't contain a line break"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := field(t, tt.key).Validate(tt.value)
			if tt.err == "" {
				assert.NoError(t, err)
				return
			}
			assert.EqualError(t, err, tt.err)
		})
	}
}
