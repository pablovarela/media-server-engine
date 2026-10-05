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
			{Key: "B2_ACCOUNT_ID", Title: "B2 account ID", File: backupFile, Masked: true},
			{Key: "B2_ACCOUNT_KEY", Title: "B2 account key", File: backupFile, Masked: true},
		}},
		{Name: "VPN", Summary: "provider, user, password, countries", Fields: []Field{
			{Key: "VPN_SERVICE_PROVIDER", Title: "VPN provider (as gluetun names it)", File: vpnFile},
			{Key: "OPENVPN_USER", Title: "OpenVPN user", File: vpnFile, Masked: true},
			{Key: "OPENVPN_PASSWORD", Title: "OpenVPN password", File: vpnFile, Masked: true},
			{Key: "SERVER_COUNTRIES", Title: "Server countries, comma-separated", File: vpnFile, Optional: true},
		}},
		{Name: "Healthchecks", Summary: "ping, API and manage keys", Fields: []Field{
			{Key: "HEALTHCHECKS_PING_KEY", Title: "Ping key", File: healthchecksFile, Masked: true},
			{Key: "HEALTHCHECKS_API_KEY", Title: "Read-only API key", File: healthchecksFile, Masked: true},
			{Key: "HEALTHCHECKS_MANAGE_KEY", Title: "API key", File: healthchecksFile, Masked: true},
		}},
		{Name: "App logins", Summary: "Jellyfin, Deluge, Portainer passwords", Fields: []Field{
			{Key: "JELLYFIN_ADMIN_PASSWORD", Title: "Jellyfin admin password", File: AppsFile, Masked: true},
			{Key: "DELUGE_WEB_PASSWORD", Title: "Deluge web password", File: AppsFile, Masked: true},
			{Key: "PORTAINER_ADMIN_PASSWORD", Title: "Portainer admin password", File: AppsFile, Masked: true},
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

func timeZone(value string) error {
	if _, err := time.LoadLocation(value); err != nil {
		return fmt.Errorf("%s isn't a time zone", value)
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
