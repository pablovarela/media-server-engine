package wiring

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	defaultWait = 5 * time.Minute
	defaultPoll = 2 * time.Second
)

type Env struct {
	Settings map[string]string
	Secrets  map[string]string
	Config   string
	Data     string
	HTTP     *http.Client
	Pause    func(ctx context.Context, d time.Duration) error
	Say      func(line string)
	Redact   *Redactor
	changes  *int
}

func (e Env) URL(variable, fallback string) string {
	if address := e.Settings[variable]; address != "" {
		return strings.TrimRight(address, "/")
	}
	return fallback
}

func (e Env) Declared(name string) (map[string]any, error) {
	content, err := os.ReadFile(filepath.Join(e.Config, name)) //nolint:gosec // the config's own declarations
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	declared := map[string]any{}
	if err := yaml.Unmarshal(content, &declared); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if declared == nil {
		declared = map[string]any{}
	}
	return declared, nil
}

func (e Env) State() State {
	return State{Dir: filepath.Join(e.Data, "volumes", ".wiring")}
}

func (e Env) Change(app, line string) {
	if e.changes != nil {
		*e.changes++
	}
	e.Say(app + ": " + line)
}

func (e Env) Changes() int {
	if e.changes == nil {
		return 0
	}
	return *e.changes
}

type Step struct {
	Name string
	Apps []string
	Run  func(ctx context.Context, env Env) error
}

type Health struct {
	Variable string
	Fallback string
	Path     string
}

type Wiring struct {
	Env    Env
	Steps  []Step
	Health map[string]Health
	Wait   time.Duration
	Poll   time.Duration
	Now    func() time.Time
	Warn   func(line string)
}

func (w *Wiring) wait() time.Duration {
	if w.Wait == 0 {
		return defaultWait
	}
	return w.Wait
}

func (w *Wiring) poll() time.Duration {
	if w.Poll == 0 {
		return defaultPoll
	}
	return w.Poll
}

func (w *Wiring) Wire(ctx context.Context) (string, error) {
	if w.Env.changes == nil {
		w.Env.changes = new(int)
	}
	deadline := w.Now().Add(w.wait())
	var failed []string
	for _, step := range w.Steps {
		ok, err := w.answering(ctx, step.Apps, deadline)
		if err != nil {
			return "", err
		}
		if !ok {
			w.Warn(fmt.Sprintf("%s: %s not answering after %.0fs; skipped its wiring", step.Name, strings.Join(step.Apps, " and "), w.wait().Seconds()))
			failed = append(failed, step.Name)
			continue
		}
		if err := step.Run(ctx, w.Env); err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			w.Warn(w.Env.Redact.Hide(step.Name + ": " + err.Error()))
			failed = append(failed, step.Name)
		}
	}
	if len(failed) > 0 {
		return "", errors.New("wiring failed: " + strings.Join(failed, " "))
	}
	return changesMade(w.Env.Changes()), nil
}

func changesMade(n int) string {
	switch n {
	case 0:
		return "nothing to change"
	case 1:
		return "1 change"
	}
	return fmt.Sprintf("%d changes", n)
}

func (w *Wiring) answering(ctx context.Context, apps []string, deadline time.Time) (bool, error) {
	for _, app := range apps {
		health := w.Health[app]
		address := w.Env.URL(health.Variable, health.Fallback) + health.Path
		for !w.answers(ctx, address) {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			if !w.Now().Before(deadline) {
				return false, nil
			}
			if err := w.Env.Pause(ctx, w.poll()); err != nil {
				return false, err
			}
		}
	}
	return true, nil
}

func (w *Wiring) answers(ctx context.Context, address string) bool {
	timed, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(timed, http.MethodGet, address, nil)
	if err != nil {
		return false
	}
	response, err := w.Env.HTTP.Do(request)
	if err != nil {
		return false
	}
	_ = response.Body.Close()
	return response.StatusCode < http.StatusBadRequest
}
