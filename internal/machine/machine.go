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
	return "no answer within " + env.timeout().String()
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
	Name   string
	Status Status
	Detail string
	Fix    string
}

type Progress interface {
	Started(name string)
	Finished(result Result)
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

func Begin(name string) string {
	return "  " + name + "..."
}

func End(result Result, p *paint.Painter) string {
	mark := map[Status]string{Pass: p.Success("✓"), Fail: p.Failure("✗"), Skip: p.Faint("–"), Unchecked: p.Warning("?")}[result.Status]
	line := " " + mark
	if result.Detail != "" {
		line += " " + result.Detail
	}
	line += "\n"
	if result.Status == Fail && result.Fix != "" {
		line += "      run: " + result.Fix + "\n"
	}
	return line
}

func (r Report) Closing() string {
	problems, unchecked := r.Problems(), r.Unchecked()
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
	ok          bool
	detail, fix string
}

type check struct {
	id      string
	name    func(Env) string
	needs   []string
	applies func(Env) bool
	probe   func(ctx context.Context, env Env) outcome
}

const noSystemd = "no systemd here, so nothing runs unattended; run mse update --apply yourself"

type silent struct{}

func (silent) Started(string)  {}
func (silent) Finished(Result) {}

func Run(ctx context.Context, env Env) Report {
	return RunEach(ctx, env, silent{})
}

func RunEach(ctx context.Context, env Env, progress Progress) Report {
	var report Report
	finish := func(result Result) {
		report.Results = append(report.Results, result)
		progress.Finished(result)
	}
	passed := map[string]bool{}
	for _, c := range checks() {
		if c.applies != nil && !c.applies(env) {
			passed[c.id] = true
			continue
		}
		name := c.name(env)
		progress.Started(name)
		if blocker := firstUnmet(c.needs, passed); blocker != "" {
			finish(Result{Name: name, Status: Skip, Detail: "skipped until " + skipReason(blocker, env)})
			continue
		}
		result := probed(ctx, env, c, name)
		passed[c.id] = result.Status == Pass
		finish(result)
	}
	portsChecks(ctx, env, passed, progress, finish)
	if !env.Systemd {
		progress.Started("timers")
		finish(Result{Name: "timers", Status: Skip, Detail: noSystemd})
	}
	return report
}

func probed(ctx context.Context, env Env, c check, name string) Result {
	probing, cancel := env.bounded(ctx)
	defer cancel()
	result := c.probe(probing, env)
	switch {
	case result.ok:
		return Result{Name: name, Status: Pass, Detail: result.detail}
	case probing.Err() != nil:
		return Result{Name: name, Status: Unchecked, Detail: env.noAnswer()}
	}
	return Result{Name: name, Status: Fail, Detail: result.detail, Fix: result.fix}
}

func portsChecks(ctx context.Context, env Env, passed map[string]bool, progress Progress, finish func(Result)) {
	if len(env.Ports.Unreadable) > 0 {
		progress.Started(readableName)
		finish(unreadablePorts(env.Ports.Unreadable))
	}
	if len(env.Ports.Ports) == 0 {
		return
	}
	progress.Started(portsName)
	if !passed[sessionCheck] {
		finish(Result{Name: portsName, Status: Skip, Detail: "skipped until " + skipReason(sessionCheck, env)})
		return
	}
	probing, cancel := env.bounded(ctx)
	defer cancel()
	finish(portsResult(probing, env.Ports, env.noAnswer()))
}

func firstUnmet(needs []string, passed map[string]bool) string {
	for _, need := range needs {
		if !passed[need] {
			return need
		}
	}
	return ""
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
