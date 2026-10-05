package machine

import (
	"context"
	"fmt"
	"time"

	"github.com/pablovarela/media-server-engine/internal/paint"
	"github.com/pablovarela/media-server-engine/internal/process"
)

type runner interface {
	Output(ctx context.Context, c process.Command) (process.Result, error)
}

type Env struct {
	Runner   runner
	Account  string
	Home     string
	GOOS     string
	Systemd  bool
	ProcRoot string
	Ports    PortsCheck
	Carried  []string
	Getenv   func(string) string
	Timeout  time.Duration
}

const defaultTimeout = 30 * time.Second

func (env Env) timeout() time.Duration {
	if env.Timeout == 0 {
		return defaultTimeout
	}
	return env.Timeout
}

func (env Env) noAnswer() string {
	return "(no answer within " + env.timeout().String() + ")"
}

func (env Env) bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, env.timeout())
}

type Status int

const (
	Pass Status = iota
	Fail
	Skip
	Unchecked
)

type Result struct {
	Status Status
	Line   string
	Fix    string
}

type Report struct {
	Results []Result
}

func (r Report) Problems() int { return r.count(Fail) }

func (r Report) Unchecked() int { return r.count(Unchecked) }

func (r Report) Ready() bool { return r.Problems() == 0 && r.Unchecked() == 0 }

func (r Report) count(status Status) int {
	n := 0
	for _, result := range r.Results {
		if result.Status == status {
			n++
		}
	}
	return n
}

func rendered(result Result, p *paint.Painter) string {
	switch result.Status {
	case Fail:
		return fmt.Sprintf("  %s %s\n      run: %s\n", p.Failure("✗"), result.Line, result.Fix)
	case Skip:
		return fmt.Sprintf("  %s %s\n", p.Faint("–"), result.Line)
	case Unchecked:
		return fmt.Sprintf("  %s %s\n", p.Warning("?"), result.Line)
	}
	return fmt.Sprintf("  %s %s\n", p.Success("✓"), result.Line)
}

func closing(problems, unchecked int) string {
	switch {
	case problems == 0 && unchecked == 0:
		return "\nThis machine is ready.\n"
	case unchecked == 0:
		return fmt.Sprintf("\n%s to fix. Run mse check-machine again afterwards.\n", things(problems))
	case problems == 0:
		return fmt.Sprintf("\n%s couldn't be checked; the lines above say why. Run mse check-machine again.\n", things(unchecked))
	}
	return fmt.Sprintf("\n%s to fix, and %d couldn't be checked. Run mse check-machine again afterwards.\n", things(problems), unchecked)
}

func things(n int) string {
	if n == 1 {
		return "1 thing"
	}
	return fmt.Sprintf("%d things", n)
}

type outcome struct {
	ok        bool
	line, fix string
}

type check struct {
	id      string
	name    func(Env) string
	needs   []string
	applies func(Env) bool
	probe   func(ctx context.Context, env Env) outcome
}

const noSystemd = "timers: no systemd here, so nothing runs unattended; run mse update --apply yourself"

func Run(ctx context.Context, env Env) Report {
	return RunEach(ctx, env, func(string) {}, &paint.Painter{})
}

func RunEach(ctx context.Context, env Env, print func(string), p *paint.Painter) Report {
	var report Report
	add := func(results ...Result) {
		for _, result := range results {
			report.Results = append(report.Results, result)
			print(rendered(result, p))
		}
	}
	passed := map[string]bool{}
	for _, c := range checks() {
		if c.applies != nil && !c.applies(env) {
			passed[c.id] = true
			continue
		}
		if blocker := firstUnmet(c.needs, passed); blocker != "" {
			add(Result{Status: Skip, Line: c.name(env) + ": skipped until " + skipReason(blocker, env)})
			continue
		}
		passed[c.id] = probed(ctx, env, c, add)
	}
	add(portsCheck(ctx, env, passed)...)
	if !env.Systemd {
		add(Result{Status: Skip, Line: noSystemd})
	}
	print(closing(report.Problems(), report.Unchecked()))
	return report
}

func probed(ctx context.Context, env Env, c check, add func(...Result)) bool {
	probing, cancel := env.bounded(ctx)
	defer cancel()
	result := c.probe(probing, env)
	if !result.ok && probing.Err() != nil {
		add(Result{Status: Unchecked, Line: c.name(env) + ": couldn't check " + env.noAnswer()})
		return false
	}
	if result.ok {
		add(Result{Status: Pass, Line: orName(result.line, c.name(env))})
	} else {
		add(Result{Status: Fail, Line: result.line, Fix: result.fix})
	}
	return result.ok
}

func portsCheck(ctx context.Context, env Env, passed map[string]bool) []Result {
	switch {
	case len(env.Ports.Ports) == 0 && len(env.Ports.Unreadable) == 0:
		return nil
	case !passed[sessionCheck]:
		return []Result{{Status: Skip, Line: "ports: skipped until " + skipReason(sessionCheck, env)}}
	}
	probing, cancel := env.bounded(ctx)
	defer cancel()
	return portsResults(probing, env.Ports, env.noAnswer())
}

func firstUnmet(needs []string, passed map[string]bool) string {
	for _, need := range needs {
		if !passed[need] {
			return need
		}
	}
	return ""
}

func orName(line, name string) string {
	if line != "" {
		return line
	}
	return name
}

func skipReason(id string, env Env) string {
	return map[string]string{
		gitCheck:     "git is installed",
		ghCheck:      "gh is logged in",
		dockerCheck:  "Docker is installed",
		groupCheck:   env.Account + " is in the docker group",
		sessionCheck: "Docker answers",
		lingerCheck:  "lingering is on",
	}[id]
}
