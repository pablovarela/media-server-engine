package logfile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var colour = regexp.MustCompile("\x1b\\[[0-9;]*m")

type Options struct {
	Limit int64
	Keep  int
	RunID string
	Now   func() time.Time
	Warn  io.Writer
}

type File struct {
	mu       sync.Mutex
	options  Options
	command  string
	file     *os.File
	buffered []string
	failed   bool
}

func New(o Options) *File {
	return &File{options: o, command: "mse"}
}

func (f *File) SetCommand(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.command = name
}

func (f *File) Line(tool, text string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	prefix := ""
	if tool != "" {
		prefix = tool + " | "
	}
	line := fmt.Sprintf("%s %s[%s] %s%s\n", f.options.Now().Format("2006-01-02 15:04:05"), f.command, f.options.RunID, prefix, colour.ReplaceAllString(text, ""))
	switch {
	case f.file != nil:
		return f.write(line)
	case !f.failed:
		f.buffered = append(f.buffered, line)
	}
	return ""
}

func (f *File) Open(dir string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.file != nil || f.failed {
		return
	}
	path := filepath.Join(dir, "mse.log")
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // the installation's log directory
		f.fail(path, err)
		return
	}
	if err := rotate(dir, f.options.Limit, f.options.Keep); err != nil {
		_, _ = fmt.Fprintf(f.options.Warn, "could not rotate the log %s (%v); writing on to it\n", path, err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644) //nolint:gosec // the installation's log, readable like its other state
	if err != nil {
		f.fail(path, err)
		return
	}
	f.file = file
	for _, line := range f.buffered {
		if warning := f.write(line); warning != "" {
			_, _ = fmt.Fprintln(f.options.Warn, warning)
		}
	}
	f.buffered = nil
}

func (f *File) Opened() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.file != nil
}

func (f *File) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.file != nil {
		_ = f.file.Close()
		f.file = nil
	}
	f.buffered = nil
}

func (f *File) write(line string) string {
	if _, err := f.file.WriteString(line); err != nil {
		path := f.file.Name()
		_ = f.file.Close()
		f.file = nil
		return f.failure(path, err)
	}
	return ""
}

func (f *File) fail(path string, err error) {
	_, _ = fmt.Fprintln(f.options.Warn, f.failure(path, err))
}

func (f *File) failure(path string, err error) string {
	f.failed = true
	f.buffered = nil
	return fmt.Sprintf("could not write the log %s (%v); carrying on without it", path, err)
}
