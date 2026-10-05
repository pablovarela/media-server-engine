package machine

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func ManagerWithoutDocker(ctx context.Context, r runner, procRoot, account string) (uid string, stale bool) {
	groups, _ := output(ctx, r, "id", "-Gn", account)
	if !slices.Contains(strings.Fields(groups), "docker") {
		return "", false
	}
	entry, _ := output(ctx, r, "getent", "group", "docker")
	docker := strings.Split(entry, ":")
	uid, _ = output(ctx, r, "id", "-u", account)
	pid, _ := output(ctx, r, "systemctl", "show", "user@"+uid+".service", "-p", "MainPID", "--value")
	if len(docker) < 3 || uid == "" || pid == "" || pid == "0" {
		return "", false
	}
	manager, found := managerGroups(filepath.Join(procRoot, pid, "status"))
	return uid, found && !slices.Contains(manager, docker[2])
}

func managerGroups(status string) ([]string, bool) {
	text, err := os.ReadFile(status) //nolint:gosec // a /proc status file named by systemd's pid
	if err != nil {
		return nil, false
	}
	for _, line := range strings.Split(string(text), "\n") {
		if groups, found := strings.CutPrefix(line, "Groups:"); found {
			return strings.Fields(groups), true
		}
	}
	return nil, false
}
