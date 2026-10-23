package timers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pablovarela/media-server-engine/internal/process"
)

type runner interface {
	Output(ctx context.Context, c process.Command) (process.Result, error)
}

type Installer struct {
	Runner runner
	Dir    string
}

type Outcome struct {
	Changed   []string
	Unchanged []string
	Removed   []string
}

func (in Installer) Install(ctx context.Context, role Role, media bool, v Values) (Outcome, error) {
	install, keep := Plan(role, media)
	var outcome Outcome
	for _, job := range install {
		changed, err := in.write(job, v)
		if err != nil {
			return Outcome{}, err
		}
		if changed {
			outcome.Changed = append(outcome.Changed, job.Unit(v.Installation))
		} else {
			outcome.Unchanged = append(outcome.Unchanged, job.Unit(v.Installation))
		}
	}
	stale, err := in.stale(v.Installation, install)
	if err != nil {
		return Outcome{}, err
	}
	for _, unit := range stale {
		if in.runsOneOf(unit, keep, v) {
			continue
		}
		if err := in.remove(ctx, unit); err != nil {
			return Outcome{}, err
		}
		outcome.Removed = append(outcome.Removed, unit)
	}
	if err := in.systemctl(ctx, "daemon-reload"); err != nil {
		return Outcome{}, err
	}
	enable := []string{"enable", "--now"}
	for _, job := range install {
		enable = append(enable, job.Unit(v.Installation)+".timer")
	}
	return outcome, in.systemctl(ctx, enable...)
}

func (in Installer) write(job Job, v Values) (bool, error) {
	if err := os.MkdirAll(in.Dir, 0o755); err != nil { //nolint:gosec // systemd reads the user's units
		return false, err
	}
	changed := false
	for n, suffix := range []string{".service", ".timer"} {
		text, err := Render(job, suffix, v)
		if err != nil {
			return false, err
		}
		path := filepath.Join(in.Dir, job.Files(v.Installation)[n])
		if current, err := os.ReadFile(path); err == nil && string(current) == text { //nolint:gosec // the user's own unit
			continue
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil { //nolint:gosec // systemd reads the user's units
			return false, err
		}
		changed = true
	}
	return changed, nil
}

func (in Installer) stale(installation string, planned []Job) ([]string, error) {
	entries, err := os.ReadDir(in.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, job := range planned {
		wanted[job.Unit(installation)] = true
	}
	var units []string
	for _, entry := range entries {
		unit, ok := unitOf(installation, entry.Name())
		if ok && !wanted[unit] && !slices.Contains(units, unit) {
			units = append(units, unit)
		}
	}
	return units, nil
}

// A unit renamed since it was installed is recognised by the command it runs.
func (in Installer) runsOneOf(unit string, keep []Job, v Values) bool {
	current, _ := os.ReadFile(filepath.Join(in.Dir, unit+".service")) //nolint:gosec // the user's own unit
	for _, job := range keep {
		if unit == job.Unit(v.Installation) {
			return true
		}
		text, err := Render(job, ".service", v)
		if err == nil && execStart(text) != "" && execStart(text) == execStart(string(current)) {
			return true
		}
	}
	return false
}

func execStart(unit string) string {
	for _, line := range strings.Split(unit, "\n") {
		if strings.HasPrefix(line, "ExecStart=") {
			return line
		}
	}
	return ""
}

func unitOf(installation, file string) (string, bool) {
	unit := strings.TrimSuffix(strings.TrimSuffix(file, ".service"), ".timer")
	return unit, unit != file && strings.HasPrefix(unit, "mse-"+installation+"-")
}

func (in Installer) remove(ctx context.Context, unit string) error {
	timer := filepath.Join(in.Dir, unit+".timer")
	if _, err := os.Stat(timer); err == nil {
		if err := in.systemctl(ctx, "disable", "--now", unit+".timer"); err != nil {
			return err
		}
	}
	for _, file := range []string{unit + ".service", unit + ".timer"} {
		if err := os.Remove(filepath.Join(in.Dir, file)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (in Installer) systemctl(ctx context.Context, args ...string) error {
	args = append([]string{"--user"}, args...)
	result, err := in.Runner.Output(ctx, process.Command{Name: "systemctl", Args: args})
	if err != nil {
		return err
	}
	if result.Exit == 0 {
		return nil
	}
	detail := strings.TrimSpace(string(result.Stderr))
	if detail == "" {
		detail = fmt.Sprintf("exit %d", result.Exit)
	}
	return fmt.Errorf("systemctl %s failed: %s", strings.Join(args, " "), detail)
}

func (o Outcome) String() string {
	if len(o.Changed)+len(o.Removed) == 0 {
		return fmt.Sprintf("all %d unchanged", len(o.Unchanged))
	}
	var parts []string
	if len(o.Changed) > 0 {
		parts = append(parts, strings.Join(o.Changed, ", ")+" changed")
	}
	if len(o.Unchanged) > 0 {
		parts = append(parts, strings.Join(o.Unchanged, ", ")+" unchanged")
	}
	if len(o.Removed) > 0 {
		parts = append(parts, strings.Join(o.Removed, ", ")+" removed")
	}
	return strings.Join(parts, "; ")
}
