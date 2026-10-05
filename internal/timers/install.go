package timers

import (
	"context"
	"fmt"
	"strings"

	"github.com/pablovarela/media-server-engine/internal/process"
)

type runner interface {
	Output(ctx context.Context, c process.Command) (process.Result, error)
}

type Installer struct {
	Runner runner
	Dir    string
	Read   func(path string) ([]byte, error)
}

type Outcome struct {
	Changed   []string
	Unchanged []string
	Removed   []string
}

func (in Installer) Install(ctx context.Context, main bool, v Values) (Outcome, error) {
	install, remove := Plan(main)
	var outcome Outcome
	for _, unit := range install {
		changed, err := in.write(ctx, unit, v)
		if err != nil {
			return Outcome{}, err
		}
		if changed {
			outcome.Changed = append(outcome.Changed, unit.Name)
		} else {
			outcome.Unchanged = append(outcome.Unchanged, unit.Name)
		}
	}
	for _, unit := range remove {
		removed, err := in.remove(ctx, unit)
		if err != nil {
			return Outcome{}, err
		}
		if removed {
			outcome.Removed = append(outcome.Removed, unit.Name)
		}
	}
	if len(outcome.Changed)+len(outcome.Removed) > 0 {
		if err := in.sudo(ctx, nil, "systemctl", "daemon-reload"); err != nil {
			return Outcome{}, err
		}
	}
	enable := []string{"systemctl", "enable", "--now"}
	for _, unit := range install {
		enable = append(enable, unit.Name+".timer")
	}
	return outcome, in.sudo(ctx, nil, enable...)
}

func (in Installer) path(file string) string {
	return in.Dir + "/" + file
}

func (in Installer) write(ctx context.Context, unit Unit, v Values) (bool, error) {
	changed := false
	for _, file := range unit.Files() {
		text, err := Render(file, v)
		if err != nil {
			return false, err
		}
		if current, err := in.Read(in.path(file)); err == nil && string(current) == text {
			continue
		}
		if err := in.sudo(ctx, strings.NewReader(text), "tee", in.path(file)); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

func (in Installer) remove(ctx context.Context, unit Unit) (bool, error) {
	if _, err := in.Read(in.path(unit.Name + ".timer")); err != nil {
		return false, nil
	}
	if err := in.sudo(ctx, nil, "systemctl", "disable", "--now", unit.Name+".timer"); err != nil {
		return false, err
	}
	files := unit.Files()
	return true, in.sudo(ctx, nil, "rm", "-f", in.path(files[0]), in.path(files[1]))
}

func (in Installer) sudo(ctx context.Context, stdin *strings.Reader, args ...string) error {
	command := process.Command{Name: "sudo", Args: args}
	if stdin != nil {
		command.Stdin = stdin
	}
	result, err := in.Runner.Output(ctx, command)
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
	return fmt.Errorf("sudo %s failed: %s", strings.Join(args, " "), detail)
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
