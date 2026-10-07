package runs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pablovarela/media-server-engine/internal/files"
)

type Run struct {
	Started time.Time `json:"started"`
	PID     int       `json:"pid"`
	Boot    string    `json:"boot,omitempty"`
	Ended   time.Time `json:"ended,omitzero"`
	Failed  bool      `json:"failed,omitempty"`
}

type State int

const (
	NeverRan State = iota
	Succeeded
	Failed
	Running
	Interrupted
)

func (r Run) State(alive func(Run) bool) State {
	switch {
	case r.Started.IsZero():
		return NeverRan
	case !r.Ended.IsZero() && r.Failed:
		return Failed
	case !r.Ended.IsZero():
		return Succeeded
	case alive(r):
		return Running
	}
	return Interrupted
}

func Alive(boot string) func(Run) bool {
	return func(r Run) bool {
		if r.PID <= 0 || r.Boot != boot {
			return false
		}
		err := syscall.Kill(r.PID, 0)
		return err == nil || errors.Is(err, syscall.EPERM)
	}
}

func BootID() string {
	id, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return strings.TrimSpace(string(id))
}

type Recorder struct {
	Dir  string
	Now  func() time.Time
	PID  int
	Boot string
}

func (r Recorder) Start(job string) error {
	return write(r.Dir, job, Run{Started: r.Now(), PID: r.PID, Boot: r.Boot})
}

func (r Recorder) Finish(job string, failed bool) error {
	run, recorded, err := Read(r.Dir, job)
	if err != nil {
		return err
	}
	now := r.Now()
	if !recorded || !run.Ended.IsZero() || run.PID != r.PID || run.Boot != r.Boot {
		run = Run{Started: now, PID: r.PID, Boot: r.Boot}
	}
	run.Ended, run.Failed = now, failed
	return write(r.Dir, job, run)
}

func Read(dir, job string) (Run, bool, error) {
	path := filepath.Join(dir, job+".json")
	text, err := os.ReadFile(path) //nolint:gosec // the installation's own run record
	if errors.Is(err, os.ErrNotExist) {
		return Run{}, false, nil
	}
	if err != nil {
		return Run{}, false, err
	}
	var run Run
	if err := json.Unmarshal(text, &run); err != nil {
		return Run{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	return run, true, nil
}

func write(dir, job string, run Run) error {
	text, err := json.Marshal(run)
	if err != nil {
		return err
	}
	return files.WriteAtomically(filepath.Join(dir, job+".json"), text, 0o644)
}
