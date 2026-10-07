package timers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pablovarela/media-server-engine/internal/process"
)

type JobStatus struct {
	Installed bool
	LastRun   time.Time
	Succeeded bool
	Next      time.Time
}

var Jobs = []Job{Update, Backup, Verify, Cleanup}

const systemdTime = "Mon 2006-01-02 15:04:05 MST"

func Status(ctx context.Context, r runner, installation string, job Job) (JobStatus, error) {
	unit := job.Unit(installation)
	result, err := r.Output(ctx, process.Command{Name: "systemctl", Args: []string{"--user", "show", unit + ".service", unit + ".timer",
		"--property=Id,LoadState,Result,ExecMainExitTimestamp,NextElapseUSecRealtime"}})
	if err != nil {
		return JobStatus{}, fmt.Errorf("read %s's state from systemd: %w", unit, err)
	}
	units := map[string]map[string]string{}
	for _, block := range strings.Split(strings.TrimSpace(string(result.Stdout)), "\n\n") {
		properties := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			if key, value, found := strings.Cut(line, "="); found {
				properties[key] = value
			}
		}
		units[properties["Id"]] = properties
	}
	service, timer := units[unit+".service"], units[unit+".timer"]
	if timer["LoadState"] != "loaded" {
		return JobStatus{}, nil
	}
	return JobStatus{
		Installed: true,
		LastRun:   parsedTime(service["ExecMainExitTimestamp"]),
		Succeeded: service["Result"] == "success",
		Next:      parsedTime(timer["NextElapseUSecRealtime"]),
	}, nil
}

func parsedTime(value string) time.Time {
	parsed, err := time.ParseInLocation(systemdTime, value, time.Local)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
