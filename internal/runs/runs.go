package runs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type Run struct {
	Started time.Time `json:"started"`
	PID     int       `json:"pid"`
	Ended   time.Time `json:"ended,omitzero"`
	Failed  bool      `json:"failed,omitempty"`
}

type State int

const (
	Succeeded State = iota
	Failed
	Running
	Interrupted
)

func (r Run) State(alive func(pid int) bool) State {
	switch {
	case !r.Ended.IsZero() && r.Failed:
		return Failed
	case !r.Ended.IsZero():
		return Succeeded
	case alive(r.PID):
		return Running
	}
	return Interrupted
}

func Alive(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

type Recorder struct {
	Dir string
	Now func() time.Time
	PID int
}

func (r Recorder) Start(job string) error {
	return write(r.Dir, job, Run{Started: r.Now(), PID: r.PID})
}

func (r Recorder) Finish(job string, failed bool) error {
	run, recorded, err := Read(r.Dir, job)
	if err != nil {
		return err
	}
	now := r.Now()
	if !recorded || !run.Ended.IsZero() {
		run = Run{Started: now, PID: r.PID}
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
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // run records are read by the user's own status command
		return err
	}
	text, err := json.Marshal(run)
	if err != nil {
		return err
	}
	staged, err := os.CreateTemp(dir, "."+job+"-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(staged.Name()) }()
	if _, err := staged.Write(text); err != nil {
		_ = staged.Close()
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}
	return os.Rename(staged.Name(), filepath.Join(dir, job+".json"))
}
