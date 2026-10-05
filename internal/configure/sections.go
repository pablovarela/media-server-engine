package configure

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
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
	neededWhen func(Values) bool
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
			{Key: "B2_ACCOUNT_ID", Title: "B2 account ID (for a b2: repository)", File: backupFile, Masked: true, Optional: true, neededWhen: b2Repository},
			{Key: "B2_ACCOUNT_KEY", Title: "B2 account key (for a b2: repository)", File: backupFile, Masked: true, Optional: true, neededWhen: b2Repository},
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
	if value == "" && f.neededWhen != nil && f.neededWhen(v) {
		return fmt.Errorf("%s: needed for a b2: repository", f.Key)
	}
	return f.Validate(value)
}

func b2Repository(v Values) bool {
	return strings.HasPrefix(v.Get(PlainFile, "RESTIC_REPOSITORY"), "b2:")
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
