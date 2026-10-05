package apply

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pablovarela/media-server-engine/internal/paint"
	"github.com/pablovarela/media-server-engine/internal/report"
)

var (
	PullAttempts = 4
	PullRetry    = 30 * time.Second
)

var errRateLimited = errors.New("a registry kept refusing pulls as too many requests; run mse update --apply again later")

type Stack interface {
	BindSources() []string
	Pull(ctx context.Context) (string, error)
	Up(ctx context.Context) (string, error)
	Reattach(ctx context.Context) (string, error)
}

type Checks interface {
	SetUp(ctx context.Context) (set, failures []string)
}

type Wiring interface {
	Wire(ctx context.Context) (string, error)
}

type Page interface {
	Reload(ctx context.Context) error
}

type Images interface {
	Prune(ctx context.Context) error
}

type Apply struct {
	Stack    Stack
	Checks   Checks
	Wiring   Wiring
	Page     Page
	Images   Images
	MkdirAll func(path string) error
	Sleep    func(time.Duration)
	Report   *report.Reporter
}

func (a *Apply) Run(ctx context.Context) error {
	a.setUpChecks(ctx)
	if err := a.createDataFolders(); err != nil {
		return err
	}
	if err := a.pull(ctx); err != nil {
		return err
	}
	if err := a.start(ctx); err != nil {
		return err
	}
	wired := a.wire(ctx)
	if err := a.Page.Reload(ctx); err != nil && wired == nil {
		return err
	}
	if wired != nil {
		return wired
	}
	return a.Images.Prune(ctx)
}

func (a *Apply) step(title string, do func() (string, error)) error {
	s := a.Report.Step(title)
	result, err := do()
	if err != nil {
		return s.Fail(err)
	}
	s.Done(result)
	return nil
}

func (a *Apply) setUpChecks(ctx context.Context) {
	if a.Checks == nil {
		return
	}
	s := a.Report.Step("Setting up the Healthchecks checks")
	set, failures := a.Checks.SetUp(ctx)
	if len(set) > 0 {
		s.Done(strings.Join(set, ", "))
	} else {
		s.Done("none set up")
	}
	for _, failure := range failures {
		a.Report.Warn(paint.Stderr.Warning("healthchecks: could not set up " + failure))
	}
}

func (a *Apply) createDataFolders() error {
	return a.step("Creating the data folders", func() (string, error) {
		for _, source := range a.Stack.BindSources() {
			if err := a.MkdirAll(source); err != nil {
				return "", err
			}
		}
		return "done", nil
	})
}

func (a *Apply) pull(ctx context.Context) error {
	return a.step("Pulling images", func() (string, error) {
		for attempt := 1; ; attempt++ {
			result, err := a.Stack.Pull(ctx)
			if err == nil || !rateLimited(err) {
				return result, err
			}
			if attempt == PullAttempts {
				return "", errRateLimited
			}
			wait := PullRetry * time.Duration(attempt)
			a.Report.Say(fmt.Sprintf("A registry is limiting requests; trying the pull again in %s...", wait))
			a.Sleep(wait)
		}
	})
}

func rateLimited(err error) bool {
	return strings.Contains(err.Error(), "toomanyrequests") || strings.Contains(err.Error(), "Too Many Requests")
}

func (a *Apply) start(ctx context.Context) error {
	started := a.step("Starting the stack", func() (string, error) { return a.Stack.Up(ctx) })
	reattached := a.step("Reattaching to gluetun", func() (string, error) { return a.Stack.Reattach(ctx) })
	if started != nil {
		return started
	}
	return reattached
}

func (a *Apply) wire(ctx context.Context) error {
	if a.Wiring == nil {
		return nil
	}
	return a.step("Wiring the apps", func() (string, error) { return a.Wiring.Wire(ctx) })
}
