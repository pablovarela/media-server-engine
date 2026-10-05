package timers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

func (in Installer) Install(ctx context.Context, role Role, v Values) (Outcome, error) {
	install, remove := Plan(role)
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
	for _, job := range remove {
		removed, err := in.remove(ctx, job, v.Installation)
		if err != nil {
			return Outcome{}, err
		}
		if removed {
			outcome.Removed = append(outcome.Removed, job.Unit(v.Installation))
		}
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

func (in Installer) remove(ctx context.Context, job Job, installation string) (bool, error) {
	files := job.Files(installation)
	if !in.present(files) {
		return false, nil
	}
	if _, err := os.Stat(filepath.Join(in.Dir, files[1])); err == nil {
		if err := in.systemctl(ctx, "disable", "--now", files[1]); err != nil {
			return false, err
		}
	}
	for _, file := range files {
		if err := os.Remove(filepath.Join(in.Dir, file)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	return true, nil
}

func (in Installer) present(files []string) bool {
	for _, file := range files {
		if _, err := os.Stat(filepath.Join(in.Dir, file)); err == nil {
			return true
		}
	}
	return false
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
		parts = append(parts, strings.Join(o.Removed, ", ")+" removed (not the main)")
	}
	return strings.Join(parts, "; ")
}
