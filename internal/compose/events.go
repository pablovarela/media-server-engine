package compose

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/docker/compose/v5/pkg/api"
)

type Outcome struct {
	Started   int
	Recreated int
	Stopped   int
	Restarted int
	Removed   int
	Pulled    int
	Running   int
	Failed    []string
}

func (o Outcome) String() string {
	var parts []string
	add := func(n int, format, one, many string) {
		if n == 0 {
			return
		}
		noun := many
		if n == 1 {
			noun = one
		}
		parts = append(parts, fmt.Sprintf(format, n, noun))
	}
	add(o.Pulled, "pulled %d %s", "image", "images")
	add(o.Stopped, "stopped %d %s", "service", "services")
	if o.Started > 0 {
		started := fmt.Sprintf("started %d %s", o.Started, map[bool]string{true: "service", false: "services"}[o.Started == 1])
		if o.Recreated > 0 {
			started += fmt.Sprintf(" (%d recreated)", o.Recreated)
		}
		parts = append(parts, started)
	}
	add(o.Restarted, "restarted %d %s", "service", "services")
	add(o.Removed, "removed %d %s", "container", "containers")
	add(o.Running, "%d already %s", "running", "running")
	if len(parts) == 0 {
		return "done"
	}
	return strings.Join(parts, ", ")
}

type Outcomes struct {
	mu      sync.Mutex
	current Outcome
}

func (o *Outcomes) Take() Outcome {
	o.mu.Lock()
	defer o.mu.Unlock()
	taken := o.current
	o.current = Outcome{}
	return taken
}

func (o *Outcomes) count(e api.Resource) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if e.Status == api.Error {
		o.current.Failed = append(o.current.Failed, strings.TrimPrefix(e.ID, "Container ")+": "+e.Details)
		return
	}
	if counter := o.counterFor(e); e.Status == api.Done && counter != nil {
		*counter++
	}
}

func (o *Outcomes) counterFor(e api.Resource) *int {
	if strings.HasPrefix(e.ID, "Image ") && e.Text == api.StatusPulled {
		return &o.current.Pulled
	}
	if !strings.HasPrefix(e.ID, "Container ") {
		return nil
	}
	return map[string]*int{
		api.StatusStarted:   &o.current.Started,
		"Recreated":         &o.current.Recreated,
		api.StatusStopped:   &o.current.Stopped,
		api.StatusRestarted: &o.current.Restarted,
		api.StatusRemoved:   &o.current.Removed,
		api.StatusRunning:   &o.current.Running,
	}[e.Text]
}

type events struct {
	tool     io.Writer
	outcomes *Outcomes
}

func (e *events) Start(context.Context, string) {}

func (e *events) On(resources ...api.Resource) {
	for _, r := range resources {
		_, _ = fmt.Fprintln(e.tool, strings.TrimSpace(r.ID+" "+r.Text+" "+r.Details))
		e.outcomes.count(r)
	}
}

func (e *events) Done(string, bool) {}
