package machine

import (
	"context"
	"fmt"
	"strings"

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
}

type Status int

const (
	Pass Status = iota
	Fail
	Skip
)

type Result struct {
	Status Status
	Line   string
	Fix    string
}

type Report struct {
	Results []Result
}

func (r Report) Problems() int {
	problems := 0
	for _, result := range r.Results {
		if result.Status == Fail {
			problems++
		}
	}
	return problems
}

func (r Report) Render(p *paint.Painter) string {
	var out strings.Builder
	for _, result := range r.Results {
		switch result.Status {
		case Pass:
			fmt.Fprintf(&out, "  %s %s\n", p.Success("✓"), result.Line)
		case Fail:
			fmt.Fprintf(&out, "  %s %s\n      run: %s\n", p.Failure("✗"), result.Line, result.Fix)
		case Skip:
			fmt.Fprintf(&out, "  %s %s\n", p.Faint("–"), result.Line)
		}
	}
	switch problems := r.Problems(); problems {
	case 0:
		out.WriteString("\nThis machine is ready.\n")
	case 1:
		out.WriteString("\n1 thing to fix. Run mse check-machine again afterwards.\n")
	default:
		fmt.Fprintf(&out, "\n%d things to fix. Run mse check-machine again afterwards.\n", problems)
	}
	return out.String()
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
	passed := map[string]bool{}
	var report Report
	for _, c := range checks() {
		if c.applies != nil && !c.applies(env) {
			passed[c.id] = true
			continue
		}
		if blocker := firstUnmet(c.needs, passed); blocker != "" {
			report.Results = append(report.Results, Result{Status: Skip, Line: c.name(env) + ": skipped until " + skipReason(blocker, env)})
			continue
		}
		result := c.probe(ctx, env)
		passed[c.id] = result.ok
		if result.ok {
			report.Results = append(report.Results, Result{Status: Pass, Line: orName(result.line, c.name(env))})
		} else {
			report.Results = append(report.Results, Result{Status: Fail, Line: result.line, Fix: result.fix})
		}
	}
	report.Results = append(report.Results, portsCheck(ctx, env, passed)...)
	if !env.Systemd {
		report.Results = append(report.Results, Result{Status: Skip, Line: noSystemd})
	}
	return report
}

func portsCheck(ctx context.Context, env Env, passed map[string]bool) []Result {
	switch {
	case len(env.Ports.Ports) == 0:
		return nil
	case !passed[sessionCheck]:
		return []Result{{Status: Skip, Line: "ports: skipped until " + skipReason(sessionCheck, env)}}
	}
	return portsResults(ctx, env.Ports)
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
