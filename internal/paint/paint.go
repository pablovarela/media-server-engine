package paint

import (
	"sync"

	"github.com/fatih/color"
)

var enabled bool

func Enable(on bool) { enabled = on }

func Detect() { Enable(!color.NoColor) }

func Success(text string) string { return paint(text, color.FgGreen) }

func Warning(text string) string { return paint(text, color.FgYellow) }

func Failure(text string) string { return paint(text, color.FgRed) }

func Bold(text string) string { return paint(text, color.Bold) }

func paint(text string, attribute color.Attribute) string {
	if !enabled || text == "" {
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
	return paint(name, attribute)
}
