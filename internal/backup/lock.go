package backup

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

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
