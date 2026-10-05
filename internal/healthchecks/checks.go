package healthchecks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const ChecksURL = "https://healthchecks.io/api/v3/checks/"

const hour = 3600

const (
	backupJob = "backup"
	updateJob = "update"
	verifyJob = "verify"
)

type Facts struct {
	Name       string
	TimeZone   string
	Repository string
	SSH        string
}

type Check struct {
	Job  string
	Slug string
}

type schedule struct {
	cron    string
	grace   int
	command string
}

var schedules = map[string]schedule{
	backupJob: {cron: "30 4 * * *", grace: 2 * hour, command: "mse backup"},
	updateJob: {cron: "0 5 * * *", grace: 2 * hour, command: "mse update --apply"},
	verifyJob: {cron: "30 5 * * 0", grace: 4 * hour, command: "mse verify-backup"},
}

var weekdays = []string{"Sundays", "Mondays", "Tuesdays", "Wednesdays", "Thursdays", "Fridays", "Saturdays"}

func ChecksFor(name, role, shortHost string) []Check {
	jobs := []string{updateJob}
	if role == "main" {
		jobs = []string{backupJob, verifyJob, updateJob}
	}
	checks := make([]Check, 0, len(jobs))
	for _, job := range jobs {
		checks = append(checks, Check{Job: job, Slug: Slug(name, job, role, shortHost)})
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
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, m.URL, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("X-Api-Key", m.Key)
	request.Header.Set("Content-Type", "application/json")
	response, err := m.Client.Do(request)
	if err != nil {
		return withoutAddress(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusBadRequest {
		return nil
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
		"name": check.Slug, "slug": check.Slug, "tags": check.Job, "desc": description(check.Job, facts),
		"schedule": s.cron, "tz": zone, "grace": s.grace, "channels": "*", "unique": []string{"slug"},
	}
}

func description(job string, facts Facts) string {
	s := schedules[job]
	runs := "Runs " + when(s.cron)
	unit := "mse-" + facts.Name + "-" + job
	timer := fmt.Sprintf("via the %s timer (%s)", unit, s.command)
	login := fmt.Sprintf("If it fails: ssh %s, journalctl --user -u %s.service", facts.SSH, unit)
	switch job {
	case backupJob:
		return fmt.Sprintf("Nightly backup of %s's app state (libraries, history, users, settings) to %s with restic. Media files are not included. "+
			"%s on the main machine %s. %s --since today; mse unlock-backup clears a stale lock.", facts.Name, destination(facts.Repository), runs, timer, login)
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
