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
	assert.Equal(t, []string{"General", "Backups", "Media backup", "VPN", "Healthchecks", "App logins"}, names)
	assert.Equal(t, PlainFile, field(t, "MEDIA_BACKUP").File)
	assert.True(t, field(t, "MEDIA_BACKUP").Optional)
	assert.True(t, field(t, "MEDIA_BACKUP_SCHEDULE").Optional)
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
		"a time zone":                {key: "TZ", value: "Europe/Madrid"},
		"not a time zone":            {key: "TZ", value: "Europe/Madird", err: "TZ: Europe/Madird isn't a time zone"},
		"an empty time zone":         {key: "TZ", value: "", err: "TZ: can't be empty"},
		"a port":                     {key: "HOMEPAGE_PORT", value: "8080"},
		"no port":                    {key: "HOMEPAGE_PORT", value: ""},
		"port 0":                     {key: "HOMEPAGE_PORT", value: "0", err: "HOMEPAGE_PORT: must be a number from 1 to 65535"},
		"port 70000":                 {key: "HOMEPAGE_PORT", value: "70000", err: "HOMEPAGE_PORT: must be a number from 1 to 65535"},
		"a port that's a word":       {key: "HOMEPAGE_PORT", value: "abc", err: "HOMEPAGE_PORT: must be a number from 1 to 65535"},
		"no Jellyfin user":           {key: "JELLYFIN_ADMIN_USER", value: "", err: "JELLYFIN_ADMIN_USER: can't be empty"},
		"a quote in plain text":      {key: "MEDIA_SERVER_HOST", value: "it's", err: "MEDIA_SERVER_HOST: can't contain '"},
		"a quote in a secret":        {key: "OPENVPN_PASSWORD", value: "it's"},
		"a newline":                  {key: "OPENVPN_PASSWORD", value: "a\nb", err: "OPENVPN_PASSWORD: can't contain a line break"},
		"a short Portainer password": {key: "PORTAINER_ADMIN_PASSWORD", value: "elevenchars", err: "PORTAINER_ADMIN_PASSWORD: needs at least 12 characters"},
		"a Portainer password":       {key: "PORTAINER_ADMIN_PASSWORD", value: "twelve-chars"},
		"no ping key":                {key: "HEALTHCHECKS_PING_KEY", value: ""},
		"media backup yes":           {key: "MEDIA_BACKUP", value: "yes"},
		"media backup maybe":         {key: "MEDIA_BACKUP", value: "maybe", err: "MEDIA_BACKUP: must be yes or no"},
		"no media backup setting":    {key: "MEDIA_BACKUP", value: ""},
		"no weeks kept":              {key: "MEDIA_BACKUP_KEEP_WEEKLY", value: "0", err: "MEDIA_BACKUP_KEEP_WEEKLY: must be a whole number above 0"},
		"an upload cap":              {key: "MEDIA_BACKUP_UPLOAD_LIMIT", value: "2048"},
		"a loose schedule":           {key: "MEDIA_BACKUP_SCHEDULE", value: "Sun 1am", err: "MEDIA_BACKUP_SCHEDULE: Sun 1am isn't a weekday and time like Sun 01:00"},
		"a subset without %":         {key: "MEDIA_BACKUP_CHECK_SUBSET", value: "5", err: "MEDIA_BACKUP_CHECK_SUBSET: must be a percentage above 0% and up to 100%, like 5%"},
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

func TestTheMediaRepositoryCantBeTheBackupRepository(t *testing.T) {
	v := Values{}.With(PlainFile, "RESTIC_REPOSITORY", "b2:bucket").With(PlainFile, "MEDIA_BACKUP", "yes").With(PlainFile, "MEDIA_RESTIC_REPOSITORY", "b2:bucket")

	err := field(t, "MEDIA_RESTIC_REPOSITORY").ValidateIn(v)

	assert.EqualError(t, err, "MEDIA_RESTIC_REPOSITORY: is the backup repository itself; the media needs a repository of its own")
}

func TestTheMediaRepositoryIsNeededOnlyWhenItCantBeDerived(t *testing.T) {
	tests := map[string]struct {
		apps, enabled string
		err           string
	}{
		"beside a B2 path":        {apps: "b2:bucket:restic", enabled: "yes"},
		"at a bucket root":        {apps: "b2:bucket", enabled: "yes", err: "MEDIA_RESTIC_REPOSITORY: needed when the apps repository has no path to put -media after"},
		"at a bucket root, off":   {apps: "b2:bucket", enabled: "no"},
		"at a bucket root, unset": {apps: "b2:bucket"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			v := Values{}.With(PlainFile, "RESTIC_REPOSITORY", tt.apps).With(PlainFile, "MEDIA_BACKUP", tt.enabled)

			err := field(t, "MEDIA_RESTIC_REPOSITORY").ValidateIn(v)

			if tt.err == "" {
				assert.NoError(t, err)
				return
			}
			assert.EqualError(t, err, tt.err)
		})
	}
}
