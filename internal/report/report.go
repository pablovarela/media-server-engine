package report

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
)

const failureLines = 10

type Log interface {
	Line(tool, text string)
}

type Reporter struct {
	mu      sync.Mutex
	out     io.Writer
	errOut  io.Writer
	stdout  *passing
	stderr  *passing
	log     Log
	verbose bool
	open    *Step
	tail    []string
}

type Step struct {
	r      *Reporter
	title  string
	broken bool
}

func New(out, errOut io.Writer, log Log) *Reporter {
	r := &Reporter{out: out, errOut: errOut, log: log}
	r.stdout = &passing{r: r, to: out}
	r.stderr = &passing{r: r, to: errOut}
	return r
}

func (r *Reporter) SetVerbose(on bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.verbose = on
}

func (r *Reporter) Say(line string) {
	_, _ = fmt.Fprintln(r.Stdout(), line)
}

func (r *Reporter) Warn(line string) {
	_, _ = fmt.Fprintln(r.Stderr(), line)
}

func (r *Reporter) Stdout() io.Writer { return r.stdout }
func (r *Reporter) Stderr() io.Writer { return r.stderr }

func (r *Reporter) Tool(name string) io.Writer {
	return &lineWriter{each: func(line string) { r.toolLine(name, line) }}
}

func (r *Reporter) Step(title string) *Step {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.endOpenLine()
	s := &Step{r: r, title: title}
	r.open = s
	r.tail = nil
	_, _ = fmt.Fprint(r.out, title+"...")
	r.logLine("", title+"...")
	return s
}

func (s *Step) Done(result string) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	s.finish(result)
	s.r.logLine("", s.title+"... "+result+".")
}

func (s *Step) Fail(err error) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	s.finish("failed")
	s.r.logLine("", s.title+"... failed: "+err.Error())
	for _, line := range s.r.tail {
		_, _ = fmt.Fprintln(s.r.errOut, "  "+line)
	}
	return err
}

func (s *Step) finish(result string) {
	if s.r.open == s && !s.broken {
		_, _ = fmt.Fprintln(s.r.out, " "+result+".")
	} else {
		_, _ = fmt.Fprintln(s.r.out, "  "+result+".")
	}
	if s.r.open == s {
		s.r.open = nil
	}
}

func (r *Reporter) toolLine(name, line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tail = append(r.tail, line)
	if len(r.tail) > failureLines {
		r.tail = r.tail[len(r.tail)-failureLines:]
	}
	r.logLine(name, line)
	if r.verbose {
		r.endOpenLine()
		_, _ = fmt.Fprintln(r.out, name+" | "+line)
	}
}

func (r *Reporter) endOpenLine() {
	if r.open != nil && !r.open.broken {
		_, _ = fmt.Fprintln(r.out)
		r.open.broken = true
	}
}

func (r *Reporter) logLine(tool, text string) {
	if r.log != nil {
		r.log.Line(tool, text)
	}
}

type passing struct {
	r       *Reporter
	to      io.Writer
	pending bytes.Buffer
}

func (p *passing) Write(b []byte) (int, error) {
	p.r.mu.Lock()
	defer p.r.mu.Unlock()
	p.r.endOpenLine()
	n, err := p.to.Write(b)
	p.pending.Write(b[:n])
	for {
		line, rest, found := bytes.Cut(p.pending.Bytes(), []byte("\n"))
		if !found {
			break
		}
		p.r.logLine("", string(line))
		remaining := append([]byte{}, rest...)
		p.pending.Reset()
		p.pending.Write(remaining)
	}
	return n, err
}

type lineWriter struct {
	mu      sync.Mutex
	each    func(string)
	pending strings.Builder
}

func (w *lineWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending.Write(b)
	text := w.pending.String()
	for {
		line, rest, found := strings.Cut(text, "\n")
		if !found {
			break
		}
		w.each(strings.TrimRight(line, "\r"))
		text = rest
	}
	w.pending.Reset()
	w.pending.WriteString(text)
	return len(b), nil
}
