package timers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pablovarela/media-server-engine/internal/process"
)

type JobStatus struct {
	Job     Job
	Result  string
	Running bool
	LastRun time.Time
	Next    time.Time
}

func (s JobStatus) Succeeded() bool { return s.Result == "success" }

var jobs = []Job{Update, Backup, Verify, Cleanup}

const systemdTime = "Mon 2006-01-02 15:04:05 MST"

func Status(ctx context.Context, r runner, installation string) ([]JobStatus, error) {
	args := []string{"--user", "show"}
	for _, job := range jobs {
		args = append(args, job.Files(installation)...)
	}
	args = append(args, "--property=Id,LoadState,ActiveState,Result,LastTriggerUSec,NextElapseUSecRealtime")
	result, err := r.Output(ctx, process.Command{Name: "systemctl", Args: args, Env: []string{"TZ=UTC"}})
	if err != nil {
		return nil, fmt.Errorf("read the timers from systemd: %w", err)
	}
	shown := unitProperties(string(result.Stdout))
	var statuses []JobStatus
	for _, job := range jobs {
		unit := job.Unit(installation)
		service, timer := shown[unit+".service"], shown[unit+".timer"]
		if timer["LoadState"] != "loaded" {
			continue
		}
		status := JobStatus{Job: job, Result: service["Result"], Running: service["ActiveState"] == "activating" || service["ActiveState"] == "active"}
		if status.LastRun, err = shownTime(unit+".timer", "LastTriggerUSec", timer); err != nil {
			return nil, err
		}
		if status.Next, err = shownTime(unit+".timer", "NextElapseUSecRealtime", timer); err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func unitProperties(stdout string) map[string]map[string]string {
	units := map[string]map[string]string{}
	for _, block := range strings.Split(strings.TrimSpace(stdout), "\n\n") {
		properties := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			if key, value, found := strings.Cut(line, "="); found {
				properties[key] = value
			}
		}
		units[properties["Id"]] = properties
	}
	return units
}

func shownTime(unit, property string, properties map[string]string) (time.Time, error) {
	value := properties[property]
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(systemdTime, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("read the timers from systemd: %s's %s %q isn't a time", unit, property, value)
	}
	return parsed, nil
}
