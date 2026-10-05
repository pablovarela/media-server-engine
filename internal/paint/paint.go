package paint

import (
	"os"
	"sync"

	"github.com/fatih/color"
	"golang.org/x/term"
)

type Painter struct {
	enabled bool
}

var (
	Stdout = &Painter{}
	Stderr = &Painter{}
)

func Enable(on bool) {
	Stdout.enabled = on
	Stderr.enabled = on
}

func Detect() {
	detect(os.Getenv, term.IsTerminal(int(os.Stdout.Fd())), term.IsTerminal(int(os.Stderr.Fd())))
}

func detect(getenv func(string) string, stdoutTTY, stderrTTY bool) {
	allowed := getenv("NO_COLOR") == "" && getenv("TERM") != "dumb"
	Stdout.enabled = allowed && stdoutTTY
	Stderr.enabled = allowed && stderrTTY
}

func (p *Painter) Success(text string) string { return p.paint(text, color.FgGreen) }

func (p *Painter) Warning(text string) string { return p.paint(text, color.FgYellow) }

func (p *Painter) Failure(text string) string { return p.paint(text, color.FgRed) }

func (p *Painter) Bold(text string) string { return p.paint(text, color.Bold) }

func (p *Painter) Faint(text string) string { return p.paint(text, color.Faint) }

func (p *Painter) paint(text string, attribute color.Attribute) string {
	if !p.enabled || text == "" {
		return text
	}
	c := color.New(attribute)
	c.EnableColor()
	return c.Sprint(text)
}

var servicePalette = []color.Attribute{color.FgCyan, color.FgYellow, color.FgGreen, color.FgMagenta, color.FgBlue, color.FgHiCyan, color.FgHiYellow, color.FgHiGreen, color.FgHiMagenta, color.FgHiBlue}

type Services struct {
	mu       sync.Mutex
	assigned map[string]color.Attribute
}

func (s *Services) Paint(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.assigned == nil {
		s.assigned = map[string]color.Attribute{}
	}
	attribute, ok := s.assigned[name]
	if !ok {
		attribute = servicePalette[len(s.assigned)%len(servicePalette)]
		s.assigned[name] = attribute
	}
	return Stdout.paint(name, attribute)
}
