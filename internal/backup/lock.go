package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var ErrStillRunning = errors.New("a backup is still running; run mse update --apply again once it has finished")

type Waiting struct {
	Timeout  time.Duration
	Poll     time.Duration
	Now      func() time.Time
	Sleep    func(ctx context.Context, d time.Duration) error
	Announce func()
}

func LockPath(data string) string {
	return filepath.Join(data, ".backup.lock")
}

func WaitWhileRunning(ctx context.Context, path string, w Waiting) error {
	deadline := w.Now().Add(w.Timeout)
	for announced := false; ; announced = true {
		running, err := backupRunning(path)
		if err != nil || !running {
			return err
		}
		if !w.Now().Before(deadline) {
			return ErrStillRunning
		}
		if !announced {
			w.Announce()
		}
		if err := w.Sleep(ctx, w.Poll); err != nil {
			return err
		}
	}
}

func backupRunning(path string) (bool, error) {
	file, err := os.Open(path) //nolint:gosec // the installation's backup lock
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil { //nolint:gosec // a file descriptor fits in an int
		return true, nil
	}
	return false, syscall.Flock(int(file.Fd()), syscall.LOCK_UN) //nolint:gosec // a file descriptor fits in an int
}

type heldLock struct {
	file *os.File
}

func takeLock(path string) (*heldLock, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644) //nolint:gosec // the installation's backup lock
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil { //nolint:gosec // a file descriptor fits in an int
		_ = file.Close()
		return nil, fmt.Errorf("a backup is already running (process %s)", runningProcess(path))
	}
	if err := file.Truncate(0); err != nil {
		_ = file.Close()
		return nil, err
	}
	if _, err := file.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &heldLock{file: file}, nil
}

func runningProcess(path string) string {
	text, _ := os.ReadFile(path) //nolint:gosec // the installation's backup lock
	if pid := strings.TrimSpace(string(text)); pid != "" {
		return pid
	}
	return "unknown"
}

func (l *heldLock) files() []*os.File {
	return []*os.File{l.file}
}

func (l *heldLock) release() {
	_ = l.file.Close()
}
