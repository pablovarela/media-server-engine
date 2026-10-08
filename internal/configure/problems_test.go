package configure

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProblems(t *testing.T) {
	tests := map[string]struct {
		change func(v Values) Values
		errs   []string
	}{
		"a complete config": {change: func(v Values) Values { return v }},
		"a time zone that doesn't exist": {
			change: func(v Values) Values { return v.With(PlainFile, "TZ", "Europe/Madird") },
			errs:   []string{"installation.env: TZ: Europe/Madird isn't a time zone"},
		},
		"a b2 repository without its keys": {
			change: func(v Values) Values {
				return v.With(PlainFile, "RESTIC_REPOSITORY", "b2:bucket:restic").With(backupFile, "B2_ACCOUNT_ID", "").With(backupFile, "B2_ACCOUNT_KEY", "")
			},
			errs: []string{"secrets/backup.sops.env: B2_ACCOUNT_ID: needed for a b2: repository", "secrets/backup.sops.env: B2_ACCOUNT_KEY: needed for a b2: repository"},
		},
		"no installation name": {
			change: func(v Values) Values { return v.With(PlainFile, "INSTALLATION_NAME", "") },
			errs:   []string{"installation.env: INSTALLATION_NAME: can't be empty"},
		},
		"a missing secret": {
			change: func(v Values) Values { return v.With(AppsFile, "DELUGE_WEB_PASSWORD", "") },
			errs:   []string{"secrets/apps.sops.env: DELUGE_WEB_PASSWORD: can't be empty"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var got []string
			for _, err := range Problems(tt.change(checkable())) {
				got = append(got, err.Error())
			}

			assert.Equal(t, tt.errs, got)
		})
	}
}

func checkable() Values {
	v := complete()
	for _, f := range sectionNamed("Media backup").Fields {
		v = v.With(f.File, f.Key, "")
	}
	return v.With(PlainFile, "RESTIC_REPOSITORY", "/mnt/backup").With(PlainFile, "MEDIA_SERVER_HOST", "gorgon.local").With(PlainFile, "INSTALLATION_NAME", "gorgon")
}
