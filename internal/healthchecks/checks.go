package healthchecks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const ChecksURL = "https://healthchecks.io/api/v3/checks/"

const hour = 3600

const mainRole = "main"

const (
	backupJob = "backup"
	updateJob = "update"
	verifyJob = "verify"
	mediaJob  = "media-backup"
)

type Facts struct {
	Name            string
	TimeZone        string
	Repository      string
	MediaRepository string
	SSH             string
}

type Check struct {
	Job  string
	Slug string
	Cron string
}

type schedule struct {
	cron    string
	grace   int
	command string
}

var schedules = map[string]schedule{
	backupJob: {cron: "30 4 * * *", grace: 2 * hour, command: "mse backup --apps"},
	updateJob: {cron: "0 5 * * *", grace: 2 * hour, command: "mse update --apply"},
	verifyJob: {cron: "30 5 * * 0", grace: 4 * hour, command: "mse check-backup"},
	mediaJob:  {grace: 24 * hour, command: "mse backup --media"},
}

var weekdays = []string{"Sundays", "Mondays", "Tuesdays", "Wednesdays", "Thursdays", "Fridays", "Saturdays"}

func ChecksFor(name, role, shortHost, mediaCron string) []Check {
	jobs := []string{updateJob}
	if role == mainRole {
		jobs = []string{backupJob, verifyJob, updateJob}
		if mediaCron != "" {
			jobs = []string{backupJob, verifyJob, mediaJob, updateJob}
		}
	}
	checks := make([]Check, 0, len(jobs))
	for _, job := range jobs {
		cron := schedules[job].cron
		if job == mediaJob {
			cron = mediaCron
		}
		checks = append(checks, Check{Job: job, Slug: Slug(name, job, role, shortHost), Cron: cron})
	}
	return checks
}

type Manage struct {
	Client *http.Client
	URL    string
	Key    string
}

func (m Manage) SetUp(ctx context.Context, checks []Check, facts Facts) (set, failures []string) {
	for _, check := range checks {
		if err := m.post(ctx, payload(check, facts)); err != nil {
			failures = append(failures, check.Slug+": "+err.Error())
			continue
		}
		set = append(set, check.Slug)
	}
	return set, failures
}

func (m Manage) post(ctx context.Context, body map[string]any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return m.request(ctx, http.MethodPost, m.URL, bytes.NewReader(encoded), nil)
}

func RetiredFor(name, role, mediaCron string) []string {
	if role != mainRole || mediaCron != "" {
		return nil
	}
	return []string{name + "-" + mediaJob}
}

func (m Manage) Retire(ctx context.Context, slugs []string) (removed, failures []string) {
	for _, slug := range slugs {
		gone, err := m.retire(ctx, slug)
		switch {
		case err != nil:
			failures = append(failures, slug+": "+err.Error())
		case gone:
			removed = append(removed, slug)
		}
	}
	return removed, failures
}

func (m Manage) retire(ctx context.Context, slug string) (bool, error) {
	var listed struct {
		Checks []struct {
			UUID string `json:"uuid"`
		} `json:"checks"`
	}
	if err := m.request(ctx, http.MethodGet, m.URL+"?slug="+url.QueryEscape(slug), nil, &listed); err != nil {
		return false, err
	}
	for _, check := range listed.Checks {
		if err := m.request(ctx, http.MethodDelete, m.URL+check.UUID, nil, nil); err != nil {
			return false, err
		}
	}
	return len(listed.Checks) > 0, nil
}

func (m Manage) request(ctx context.Context, method, address string, body io.Reader, into any) error {
	request, err := http.NewRequestWithContext(ctx, method, address, body)
	if err != nil {
		return err
	}
	request.Header.Set("X-Api-Key", m.Key)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := m.Client.Do(request)
	if err != nil {
		return withoutAddress(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusBadRequest {
		if into == nil {
			return nil
		}
		return json.NewDecoder(response.Body).Decode(into)
	}
	var refusal struct {
		Error string `json:"error"`
	}
	if json.NewDecoder(response.Body).Decode(&refusal) == nil && refusal.Error != "" {
		return fmt.Errorf("healthchecks.io answered %d: %s", response.StatusCode, refusal.Error)
	}
	return fmt.Errorf("healthchecks.io answered %d", response.StatusCode)
}

func payload(check Check, facts Facts) map[string]any {
	s := schedules[check.Job]
	zone := facts.TimeZone
	if zone == "" {
		zone = "Etc/UTC"
	}
	return map[string]any{
		"name": check.Slug, "slug": check.Slug, "tags": check.Job, "desc": description(check, facts),
		"schedule": check.Cron, "tz": zone, "grace": s.grace, "channels": "*", "unique": []string{"slug"},
	}
}

func description(check Check, facts Facts) string {
	job := check.Job
	s := schedules[job]
	runs := "Runs " + when(check.Cron)
	unit := "mse-" + facts.Name + "-" + job
	timer := fmt.Sprintf("via the %s timer (%s)", unit, s.command)
	login := fmt.Sprintf("If it fails: ssh %s, journalctl --user-unit %s.service", facts.SSH, unit)
	switch job {
	case backupJob:
		return fmt.Sprintf("Nightly backup of %s's app state (libraries, history, users, settings) to %s with restic. Media files are not included. "+
			"%s on the main machine %s. %s --since today; a stale lock is cleared before the next run.", facts.Name, destination(facts.Repository), runs, timer, login)
	case mediaJob:
		return fmt.Sprintf("Weekly backup of %s's media (data/media) to %s with restic, followed by a check that reads a sample of it. %s on the main machine %s. %s --since -7d -n 200.",
			facts.Name, destination(facts.MediaRepository), runs, timer, login)
	case verifyJob:
		return fmt.Sprintf("Weekly check that %s's backups in %s can be read back (restic check). %s on the main machine %s. %s --since -7d -n 200.",
			facts.Name, destination(facts.Repository), runs, timer, login)
	}
	return fmt.Sprintf("Daily update of %s: pulls the config and the newest compatible mse, pulls images and brings the apps up. %s %s. "+
		"It refuses to run while the config has uncommitted changes. %s --since today, then systemctl --user start %s.service to retry.",
		facts.Name, runs, timer, login, unit)
}

func when(cron string) string {
	fields := strings.Fields(cron)
	minute, _ := strconv.Atoi(fields[0])
	hourOfDay, _ := strconv.Atoi(fields[1])
	at := fmt.Sprintf("%02d:%02d", hourOfDay, minute)
	day, err := strconv.Atoi(fields[4])
	if err != nil {
		return "daily at " + at
	}
	return "on " + weekdays[day] + " at " + at
}

func destination(repository string) string {
	switch {
	case repository == "":
		return "its backup repository"
	case strings.HasPrefix(repository, "b2:"):
		return "Backblaze B2"
	case strings.HasPrefix(repository, "/"):
		return "a local folder"
	}
	kind, _, _ := strings.Cut(repository, ":")
	return kind + " storage"
}
