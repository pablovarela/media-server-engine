package media

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	Enabled            = "MEDIA_BACKUP"
	RepositorySetting  = "MEDIA_RESTIC_REPOSITORY"
	KeepWeeklySetting  = "MEDIA_BACKUP_KEEP_WEEKLY"
	UploadLimitSetting = "MEDIA_BACKUP_UPLOAD_LIMIT"
	ScheduleSetting    = "MEDIA_BACKUP_SCHEDULE"
	CheckSubsetSetting = "MEDIA_BACKUP_CHECK_SUBSET"
)

const (
	defaultKeepWeekly  = 4
	defaultSchedule    = "Sun 01:00"
	defaultCheckSubset = "5%"
)

var days = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

var weekly = regexp.MustCompile(`^(Sun|Mon|Tue|Wed|Thu|Fri|Sat) ([01][0-9]|2[0-3]):([0-5][0-9])$`)

type Weekly struct {
	Day          time.Weekday
	Hour, Minute int
}

func ParseWeekly(s string) (Weekly, error) {
	match := weekly.FindStringSubmatch(strings.Join(strings.Fields(s), " "))
	if match == nil {
		return Weekly{}, fmt.Errorf("%s isn't a weekday and time like %s", strings.TrimSpace(s), defaultSchedule)
	}
	hour, _ := strconv.Atoi(match[2])
	minute, _ := strconv.Atoi(match[3])
	day := 0
	for n, name := range days {
		if name == match[1] {
			day = n
		}
	}
	return Weekly{Day: time.Weekday(day), Hour: hour, Minute: minute}, nil
}

func (w Weekly) OnCalendar() string {
	return fmt.Sprintf("%s *-*-* %02d:%02d:00", days[w.Day], w.Hour, w.Minute)
}

func (w Weekly) Cron() string {
	return fmt.Sprintf("%d %d * * %d", w.Minute, w.Hour, w.Day)
}

type Settings struct {
	Enabled     bool
	Repository  string
	KeepWeekly  int
	UploadLimit int
	Schedule    Weekly
	CheckSubset string
}

func From(settings map[string]string) (Settings, error) {
	s, err := Timing(settings)
	if err != nil || !s.Enabled {
		return s, err
	}
	s.Repository, s.CheckSubset = settings[RepositorySetting], or(settings[CheckSubsetSetting], defaultCheckSubset)
	if s.Repository == "" {
		derived, ok := DefaultRepository(settings["RESTIC_REPOSITORY"])
		if !ok {
			return Settings{}, fmt.Errorf("%s: %s", RepositorySetting, NeededFor)
		}
		s.Repository = derived
	}
	if err := CheckSeparate(settings["RESTIC_REPOSITORY"], s.Repository); err != nil {
		return Settings{}, fmt.Errorf("%s: %w", RepositorySetting, err)
	}
	if s.KeepWeekly, err = number(settings, KeepWeeklySetting, defaultKeepWeekly); err != nil {
		return Settings{}, err
	}
	if s.UploadLimit, err = number(settings, UploadLimitSetting, 0); err != nil {
		return Settings{}, err
	}
	if err := CheckSubset(s.CheckSubset); err != nil {
		return Settings{}, fmt.Errorf("%s: %w", CheckSubsetSetting, err)
	}
	return s, nil
}

const NeededFor = "needed when the apps repository has no path to put -media after"

func DefaultRepository(apps string) (string, bool) {
	switch {
	case strings.HasPrefix(apps, "b2:"):
		bucketAndPath := strings.TrimPrefix(apps, "b2:")
		bucket, path, found := strings.Cut(bucketAndPath, ":")
		path = strings.TrimRight(path, "/")
		if !found || bucket == "" || path == "" {
			return "", false
		}
		return "b2:" + bucket + ":" + path + "-media", true
	case strings.HasPrefix(apps, "/"):
		path := strings.TrimRight(apps, "/")
		if path == "" {
			return "", false
		}
		return path + "-media", true
	}
	return "", false
}

func CheckSeparate(apps, media string) error {
	if normalised(apps) == normalised(media) {
		return errors.New("is the backup repository itself; the media needs a repository of its own")
	}
	return nil
}

func normalised(repository string) string {
	return strings.TrimRight(strings.TrimRight(repository, "/"), ":")
}

func CheckEnabled(value string) error {
	if value != "yes" && value != "no" {
		return errors.New("must be yes or no")
	}
	return nil
}

func CheckPositive(value string) error {
	if n, err := strconv.Atoi(value); err != nil || n < 1 {
		return errors.New("must be a whole number above 0")
	}
	return nil
}

func CheckSchedule(value string) error {
	_, err := ParseWeekly(value)
	return err
}

func CheckSubset(value string) error {
	number, isPercent := strings.CutSuffix(value, "%")
	if n, err := strconv.ParseFloat(number, 64); !isPercent || err != nil || n <= 0 || n > 100 {
		return errors.New("must be a percentage above 0% and up to 100%, like 5%")
	}
	return nil
}

func number(settings map[string]string, key string, fallback int) (int, error) {
	value := settings[key]
	if value == "" {
		return fallback, nil
	}
	if err := CheckPositive(value); err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	n, _ := strconv.Atoi(value)
	return n, nil
}

func Timing(settings map[string]string) (Settings, error) {
	if value := settings[Enabled]; value != "" {
		if err := CheckEnabled(value); err != nil {
			return Settings{}, fmt.Errorf("%s: %w", Enabled, err)
		}
	}
	if settings[Enabled] != "yes" {
		return Settings{}, nil
	}
	schedule, err := ParseWeekly(or(settings[ScheduleSetting], defaultSchedule))
	if err != nil {
		return Settings{}, fmt.Errorf("%s: %w", ScheduleSetting, err)
	}
	return Settings{Enabled: true, Schedule: schedule}, nil
}

func or(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
