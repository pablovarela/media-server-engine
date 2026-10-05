package logfile

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func rotate(dir string, limit int64, keep int) error {
	lock, err := os.OpenFile(filepath.Join(dir, ".rotate.lock"), os.O_CREATE|os.O_RDWR, 0o644) //nolint:gosec // the log directory's rotation lock
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil { //nolint:gosec // a file descriptor fits in an int
		return nil
	}
	current := filepath.Join(dir, "mse.log")
	full, err := overLimit(current, limit)
	if err != nil || !full {
		return err
	}
	if err := shiftCompressed(dir, keep); err != nil {
		return err
	}
	previous := current + ".1"
	if _, err := os.Stat(previous); err == nil {
		if err := compress(previous, numbered(dir, 2)); err != nil {
			return err
		}
	}
	return os.Rename(current, previous)
}

func overLimit(path string, limit int64) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.Size() > limit, nil
}

func shiftCompressed(dir string, keep int) error {
	if err := os.Remove(numbered(dir, keep+1)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for n := keep; n >= 2; n-- {
		if err := os.Rename(numbered(dir, n), numbered(dir, n+1)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func numbered(dir string, n int) string {
	return filepath.Join(dir, fmt.Sprintf("mse.log.%d.gz", n))
}

func compress(from, to string) error {
	if err := gzipped(from, to); err != nil {
		_ = os.Remove(to)
		return err
	}
	return os.Remove(from)
}

func gzipped(from, to string) error {
	source, err := os.Open(from) //nolint:gosec // the log rotated last time
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	target, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644) //nolint:gosec // the compressed log
	if err != nil {
		return err
	}
	writer := gzip.NewWriter(target)
	if _, err := io.Copy(writer, source); err != nil {
		_ = target.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		_ = target.Close()
		return err
	}
	return target.Close()
}
