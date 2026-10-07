package media

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheWeeklySchedule(t *testing.T) {
	tests := map[string]struct {
		given            string
		onCalendar, cron string
		err              string
	}{
		"the default":         {given: "Sun 01:00", onCalendar: "Sun *-*-* 01:00:00", cron: "0 1 * * 0"},
		"a weekday afternoon": {given: "Wed 14:35", onCalendar: "Wed *-*-* 14:35:00", cron: "35 14 * * 3"},
		"extra spaces":        {given: "  Sat   23:59 ", onCalendar: "Sat *-*-* 23:59:00", cron: "59 23 * * 6"},
		"a full day name":     {given: "Sunday 01:00", err: "Sunday 01:00 isn't a weekday and time like Sun 01:00"},
		"no time":             {given: "Sun", err: "Sun isn't a weekday and time like Sun 01:00"},
		"hour 24":             {given: "Sun 24:00", err: "Sun 24:00 isn't a weekday and time like Sun 01:00"},
		"one-digit hour":      {given: "Sun 1:00", err: "Sun 1:00 isn't a weekday and time like Sun 01:00"},
		"lower case":          {given: "sun 01:00", err: "sun 01:00 isn't a weekday and time like Sun 01:00"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			w, err := ParseWeekly(tt.given)
			if tt.err != "" {
				assert.EqualError(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.onCalendar, w.OnCalendar())
			assert.Equal(t, tt.cron, w.Cron())
		})
	}
}

func TestTheMediaRepositoryBesideTheApps(t *testing.T) {
	tests := map[string]struct {
		apps, media string
		derived     bool
	}{
		"a B2 path":          {apps: "b2:bucket:restic", media: "b2:bucket:restic-media", derived: true},
		"a nested B2 path":   {apps: "b2:bucket:backups/apps/", media: "b2:bucket:backups/apps-media", derived: true},
		"a B2 bucket root":   {apps: "b2:bucket"},
		"a B2 bucket root /": {apps: "b2:bucket:/"},
		"a local folder":     {apps: "/mnt/backup/restic", media: "/mnt/backup/restic-media", derived: true},
		"the root folder":    {apps: "/"},
		"another backend":    {apps: "s3:s3.amazonaws.com/bucket"},
		"no apps repository": {apps: ""},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			media, derived := DefaultRepository(tt.apps)

			assert.Equal(t, tt.derived, derived)
			assert.Equal(t, tt.media, media)
		})
	}
}

func TestTheSettings(t *testing.T) {
	tests := map[string]struct {
		given map[string]string
		then  Settings
		err   string
	}{
		"off when unset": {given: map[string]string{"RESTIC_REPOSITORY": "b2:bucket:restic"}, then: Settings{}},
		"off when no":    {given: map[string]string{"MEDIA_BACKUP": "no"}, then: Settings{}},
		"on with the defaults": {
			given: map[string]string{"MEDIA_BACKUP": "yes", "RESTIC_REPOSITORY": "b2:bucket:restic"},
			then:  Settings{Enabled: true, Repository: "b2:bucket:restic-media", KeepWeekly: 4, Schedule: Weekly{Day: time.Sunday, Hour: 1}, CheckSubset: "5%"},
		},
		"on with every setting": {
			given: map[string]string{
				"MEDIA_BACKUP": "yes", "RESTIC_REPOSITORY": "b2:bucket", "MEDIA_RESTIC_REPOSITORY": "b2:media:restic",
				"MEDIA_BACKUP_KEEP_WEEKLY": "8", "MEDIA_BACKUP_UPLOAD_LIMIT": "2048", "MEDIA_BACKUP_SCHEDULE": "Sat 02:30", "MEDIA_BACKUP_CHECK_SUBSET": "10%",
			},
			then: Settings{Enabled: true, Repository: "b2:media:restic", KeepWeekly: 8, UploadLimit: 2048, Schedule: Weekly{Day: time.Saturday, Hour: 2, Minute: 30}, CheckSubset: "10%"},
		},
		"on at a bucket root without a media repository": {
			given: map[string]string{"MEDIA_BACKUP": "yes", "RESTIC_REPOSITORY": "b2:bucket"},
			err:   "MEDIA_RESTIC_REPOSITORY: needed when the apps repository has no path to put -media after",
		},
		"a bad schedule": {given: map[string]string{"MEDIA_BACKUP": "yes", "RESTIC_REPOSITORY": "/b", "MEDIA_BACKUP_SCHEDULE": "Sunday"}, err: "MEDIA_BACKUP_SCHEDULE: Sunday isn't a weekday and time like Sun 01:00"},
		"zero weeks":     {given: map[string]string{"MEDIA_BACKUP": "yes", "RESTIC_REPOSITORY": "/b", "MEDIA_BACKUP_KEEP_WEEKLY": "0"}, err: "MEDIA_BACKUP_KEEP_WEEKLY: must be a whole number above 0"},
		"a bad subset":   {given: map[string]string{"MEDIA_BACKUP": "yes", "RESTIC_REPOSITORY": "/b", "MEDIA_BACKUP_CHECK_SUBSET": "150%"}, err: "MEDIA_BACKUP_CHECK_SUBSET: must be a percentage above 0% and up to 100%, like 5%"},
		"the backup repository itself": {
			given: map[string]string{"MEDIA_BACKUP": "yes", "RESTIC_REPOSITORY": "b2:bucket", "MEDIA_RESTIC_REPOSITORY": "b2:bucket:/"},
			err:   "MEDIA_RESTIC_REPOSITORY: is the backup repository itself; the media needs a repository of its own",
		},
		"neither yes nor no": {given: map[string]string{"MEDIA_BACKUP": "on"}, err: "MEDIA_BACKUP: must be yes or no"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			s, err := From(tt.given)
			if tt.err != "" {
				assert.EqualError(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.then, s)
		})
	}
}

func TestTheTimingIgnoresTheRepository(t *testing.T) {
	s, err := Timing(map[string]string{"MEDIA_BACKUP": "yes", "RESTIC_REPOSITORY": "b2:bucket", "MEDIA_BACKUP_SCHEDULE": "Sat 02:30"})

	require.NoError(t, err)
	assert.True(t, s.Enabled)
	assert.Equal(t, "Sat *-*-* 02:30:00", s.Schedule.OnCalendar())

	_, err = Timing(map[string]string{"MEDIA_BACKUP": "yes", "MEDIA_BACKUP_SCHEDULE": "Sunday"})
	assert.EqualError(t, err, "MEDIA_BACKUP_SCHEDULE: Sunday isn't a weekday and time like Sun 01:00")
}

func TestTheMediaRepositoryMustNotBeTheBackupRepository(t *testing.T) {
	tests := map[string]struct {
		apps, media string
		same        bool
	}{
		"the same":                   {apps: "b2:bucket:restic", media: "b2:bucket:restic", same: true},
		"a trailing slash":           {apps: "b2:bucket:restic", media: "b2:bucket:restic/", same: true},
		"a bucket root, written out": {apps: "b2:bucket", media: "b2:bucket:", same: true},
		"a bucket root with a slash": {apps: "b2:bucket", media: "b2:bucket:/", same: true},
		"a local folder":             {apps: "/mnt/backup/", media: "/mnt/backup", same: true},
		"beside it":                  {apps: "b2:bucket:restic", media: "b2:bucket:restic-media"},
		"inside a bucket root":       {apps: "b2:bucket", media: "b2:bucket:media"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := CheckSeparate(tt.apps, tt.media)

			assert.Equal(t, tt.same, err != nil, err)
		})
	}
}
