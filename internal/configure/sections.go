package configure

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pablovarela/media-server-engine/internal/media"
)

const (
	PlainFile        = "installation.env"
	backupFile       = "secrets/backup.sops.env"
	vpnFile          = "secrets/vpn.sops.env"
	healthchecksFile = "secrets/healthchecks.sops.env"
	AppsFile         = "secrets/apps.sops.env"
)

type Field struct {
	Key, Title string
	File       string
	Masked     bool
	Optional   bool
	check      func(string) error
	needed     func(Values) string
	against    func(value string, v Values) error
}

type Section struct {
	Name, Summary string
	Fields        []Field
}

type App struct{ Name, Key string }

var Rotatable = []App{{"Sonarr", "SONARR_API_KEY"}, {"Radarr", "RADARR_API_KEY"}, {"Prowlarr", "PROWLARR_API_KEY"}}

func Sections() []Section {
	return []Section{
		{Name: "General", Summary: "TZ, Jellyfin user, homepage port, hosts", Fields: []Field{
			{Key: "TZ", Title: "Time zone", File: PlainFile, check: timeZone},
			{Key: "JELLYFIN_ADMIN_USER", Title: "Jellyfin admin user", File: PlainFile},
			{Key: "HOMEPAGE_PORT", Title: "Homepage port (empty for the default)", File: PlainFile, Optional: true, check: port},
			{Key: "MEDIA_SERVER_HOST", Title: "Host name the links use", File: PlainFile, Optional: true},
			{Key: "HOMEPAGE_ALLOWED_HOSTS", Title: "Other host names for the homepage, comma-separated", File: PlainFile, Optional: true},
		}},
		{Name: "Backups", Summary: "restic repository, password, B2 keys", Fields: []Field{
			{Key: "RESTIC_REPOSITORY", Title: "restic repository", File: PlainFile},
			{Key: "RESTIC_PASSWORD", Title: "restic password", File: backupFile, Masked: true},
			{Key: "B2_ACCOUNT_ID", Title: "B2 account ID (for a b2: repository)", File: backupFile, Masked: true, Optional: true, needed: b2Repository},
			{Key: "B2_ACCOUNT_KEY", Title: "B2 account key (for a b2: repository)", File: backupFile, Masked: true, Optional: true, needed: b2Repository},
		}},
		{Name: "Media backup", Summary: "on or off, repository, weeks kept, upload cap, schedule", Fields: []Field{
			{Key: media.Enabled, Title: "Back up the media too? yes or no (empty for no)", File: PlainFile, Optional: true, check: media.CheckEnabled},
			{Key: media.RepositorySetting, Title: "Media restic repository (empty for the backup repository with -media after it)", File: PlainFile, Optional: true, needed: mediaRepositoryUnderivable, against: notTheBackupRepository},
			{Key: media.WeeksKeptSetting, Title: "Weekly media snapshots to keep (empty for 4)", File: PlainFile, Optional: true, check: media.CheckPositive},
			{Key: media.UploadLimitSetting, Title: "Upload cap in KiB/s (empty for none)", File: PlainFile, Optional: true, check: media.CheckPositive},
			{Key: media.ScheduleSetting, Title: "When it runs, weekday and time (empty for Sun 01:00)", File: PlainFile, Optional: true, check: media.CheckSchedule},
			{Key: media.CheckSubsetSetting, Title: "Share of the media each run reads back (empty for 5%)", File: PlainFile, Optional: true, check: media.CheckSubset},
		}},
		{Name: "VPN", Summary: "provider, user, password, countries", Fields: []Field{
			{Key: "VPN_SERVICE_PROVIDER", Title: "VPN provider (as gluetun names it)", File: vpnFile},
			{Key: "OPENVPN_USER", Title: "OpenVPN user (none for WireGuard)", File: vpnFile, Masked: true, Optional: true},
			{Key: "OPENVPN_PASSWORD", Title: "OpenVPN password (none for WireGuard)", File: vpnFile, Masked: true, Optional: true},
			{Key: "SERVER_COUNTRIES", Title: "Server countries, comma-separated", File: vpnFile, Optional: true},
		}},
		{Name: "Healthchecks", Summary: "ping, API and manage keys", Fields: []Field{
			{Key: "HEALTHCHECKS_PING_KEY", Title: "Ping key (without one there are no pings)", File: healthchecksFile, Masked: true, Optional: true},
			{Key: "HEALTHCHECKS_API_KEY", Title: "Read-only API key (shows the checks on the landing page)", File: healthchecksFile, Masked: true, Optional: true},
			{Key: "HEALTHCHECKS_MANAGE_KEY", Title: "Read-write API key (sets up the checks)", File: healthchecksFile, Masked: true, Optional: true},
		}},
		{Name: "App logins", Summary: "Jellyfin, Deluge, Portainer passwords", Fields: []Field{
			{Key: "JELLYFIN_ADMIN_PASSWORD", Title: "Jellyfin admin password", File: AppsFile, Masked: true},
			{Key: "DELUGE_WEB_PASSWORD", Title: "Deluge web password", File: AppsFile, Masked: true},
			{Key: "PORTAINER_ADMIN_PASSWORD", Title: "Portainer admin password (at least 12 characters)", File: AppsFile, Masked: true, check: atLeast12},
		}},
	}
}

func (f Field) Validate(value string) error {
	switch {
	case strings.ContainsAny(value, "\r\n"):
		return fmt.Errorf("%s: can't contain a line break", f.Key)
	case f.File == PlainFile && strings.Contains(value, "'"):
		return fmt.Errorf("%s: can't contain '", f.Key)
	case value == "" && f.Optional:
		return nil
	case value == "":
		return fmt.Errorf("%s: can't be empty", f.Key)
	case f.check != nil:
		if err := f.check(value); err != nil {
			return fmt.Errorf("%s: %w", f.Key, err)
		}
	}
	return nil
}

func (f Field) ValidateIn(v Values) error {
	value := v.Get(f.File, f.Key)
	if value == "" && f.needed != nil {
		if reason := f.needed(v); reason != "" {
			return fmt.Errorf("%s: %s", f.Key, reason)
		}
	}
	if value != "" && f.against != nil {
		if err := f.against(value, v); err != nil {
			return fmt.Errorf("%s: %w", f.Key, err)
		}
	}
	return f.Validate(value)
}

func b2Repository(v Values) string {
	if strings.HasPrefix(v.Get(PlainFile, "RESTIC_REPOSITORY"), "b2:") {
		return "needed for a b2: repository"
	}
	return ""
}

func notTheBackupRepository(repository string, v Values) error {
	return media.CheckSeparate(v.Get(PlainFile, "RESTIC_REPOSITORY"), repository)
}

func mediaRepositoryUnderivable(v Values) string {
	if v.Get(PlainFile, media.Enabled) != "yes" {
		return ""
	}
	if _, derived := media.DefaultRepository(v.Get(PlainFile, "RESTIC_REPOSITORY")); derived {
		return ""
	}
	return media.NeededFor
}

func timeZone(value string) error {
	if _, err := time.LoadLocation(value); err != nil {
		return fmt.Errorf("%s isn't a time zone", value)
	}
	return nil
}

func atLeast12(value string) error {
	if len([]rune(value)) < 12 {
		return errors.New("needs at least 12 characters")
	}
	return nil
}

func port(value string) error {
	if n, err := strconv.Atoi(value); err != nil || n < 1 || n > 65535 {
		return errors.New("must be a number from 1 to 65535")
	}
	return nil
}

func secretFiles() []string {
	var files []string
	for _, section := range Sections() {
		for _, f := range section.Fields {
			if f.File != PlainFile && !slices.Contains(files, f.File) {
				files = append(files, f.File)
			}
		}
	}
	return files
}
