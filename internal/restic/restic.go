package restic

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pablovarela/media-server-engine/internal/process"
)

const (
	retryLock    = "--retry-lock"
	waitForLocks = "2h"
)

const (
	exitNoRepository = 10
	exitLocked       = 11
)

type Runner interface {
	Run(ctx context.Context, c process.Command) (int, error)
	Output(ctx context.Context, c process.Command) (process.Result, error)
}

type Restic struct {
	Runner Runner
	Env    []string
}

type Snapshot struct {
	ID       string    `json:"short_id"`
	Time     time.Time `json:"time"`
	Hostname string    `json:"hostname"`
	Tags     []string  `json:"tags"`
}

func (s Snapshot) Tag(key string) string {
	for _, tag := range s.Tags {
		if name, value, found := strings.Cut(tag, ":"); found && name == key {
			return value
		}
	}
	return ""
}

type Lock struct {
	Exclusive bool      `json:"exclusive"`
	Hostname  string    `json:"hostname"`
	PID       int       `json:"pid"`
	Time      time.Time `json:"time"`
}

func (l Lock) String() string {
	kind, host := "shared", l.Hostname
	if l.Exclusive {
		kind = "exclusive"
	}
	if host == "" {
		host = "an unknown host"
	}
	return fmt.Sprintf("%s lock from %s (process %d) since %s", kind, host, l.PID, l.Time.Format("2006-01-02 15:04"))
}

type LockedError struct {
	Locks []Lock
}

func (e *LockedError) Error() string {
	var message strings.Builder
	message.WriteString("restic gave up waiting for a lock on the backup repository. Locks held:")
	for _, lock := range e.Locks {
		message.WriteString("\n  " + lock.String())
	}
	message.WriteString("\nIf none of those machines is running restic now, remove every lock with: mse unlock-backup --all")
	return message.String()
}

type BackupOptions struct {
	Host        string
	Tags        []string
	ExcludeFile string
	Dir         string
	Paths       []string
	Inherit     []*os.File
}

type RestoreOptions struct {
	Snapshot string
	Host     string
	Target   string
	Include  []string
	Exclude  []string
}

func (r Restic) Init(ctx context.Context) error {
	return r.run(ctx, process.Command{Args: []string{"init"}})
}
func (r Restic) Unlock(ctx context.Context) error {
	return r.run(ctx, process.Command{Args: []string{"unlock"}})
}
func (r Restic) UnlockAll(ctx context.Context) error {
	return r.run(ctx, process.Command{Args: []string{"unlock", "--remove-all"}})
}
func (r Restic) Check(ctx context.Context) error {
	return r.run(ctx, process.Command{Args: []string{"check", retryLock, waitForLocks}})
}

func (r Restic) Backup(ctx context.Context, o BackupOptions) error {
	args := []string{"backup", retryLock, waitForLocks, "--host", o.Host}
	for _, tag := range o.Tags {
		args = append(args, "--tag", tag)
	}
	args = append(args, "--exclude-file", o.ExcludeFile)
	return r.run(ctx, process.Command{Args: append(args, o.Paths...), Dir: o.Dir, ExtraFiles: o.Inherit})
}

func (r Restic) Forget(ctx context.Context, host string, inherit []*os.File) error {
	return r.run(ctx, process.Command{ExtraFiles: inherit, Args: []string{
		"forget", retryLock, waitForLocks, "--host", host, "--prune", "--keep-daily", "7", "--keep-weekly", "4", "--keep-monthly", "6",
	}})
}

func (r Restic) Restore(ctx context.Context, o RestoreOptions) error {
	args := []string{"restore", retryLock, waitForLocks, o.Snapshot}
	if o.Host != "" {
		args = append(args, "--host", o.Host)
	}
	args = append(args, "--target", o.Target)
	for _, pattern := range o.Include {
		args = append(args, "--include", pattern)
	}
	for _, pattern := range o.Exclude {
		args = append(args, "--exclude", pattern)
	}
	return r.run(ctx, process.Command{Args: args})
}

func (r Restic) HasRepository(ctx context.Context) (bool, error) {
	result, err := r.output(ctx, "cat", "config")
	switch {
	case err != nil:
		return false, err
	case result.Exit == exitNoRepository:
		return false, nil
	case result.Exit != 0:
		return false, failure("cat", result)
	}
	return true, nil
}

func (r Restic) Snapshots(ctx context.Context, host string) ([]Snapshot, error) {
	result, err := r.output(ctx, "snapshots", "--no-lock", "--host", host, "--json")
	switch {
	case err != nil:
		return nil, err
	case result.Exit == exitNoRepository:
		return nil, nil
	case result.Exit != 0:
		return nil, failure("snapshots", result)
	}
	var snapshots []Snapshot
	if err := json.Unmarshal(result.Stdout, &snapshots); err != nil {
		return nil, fmt.Errorf("restic snapshots printed unreadable JSON: %w", err)
	}
	return snapshots, nil
}

func (r Restic) Locks(ctx context.Context) ([]Lock, error) {
	listed, err := r.output(ctx, "list", "locks", "--no-lock")
	if err != nil {
		return nil, err
	}
	if listed.Exit != 0 {
		return nil, failure("list", listed)
	}
	var locks []Lock
	for _, id := range strings.Fields(string(listed.Stdout)) {
		shown, err := r.output(ctx, "cat", "lock", id, "--no-lock")
		var lock Lock
		if err != nil || shown.Exit != 0 || json.Unmarshal(shown.Stdout, &lock) != nil {
			continue
		}
		locks = append(locks, lock)
	}
	return locks, nil
}

func (r Restic) run(ctx context.Context, c process.Command) error {
	c.Name, c.Env = "restic", r.Env
	exit, err := r.Runner.Run(ctx, c)
	switch {
	case err != nil:
		return err
	case exit == exitLocked:
		locks, _ := r.Locks(ctx)
		return &LockedError{Locks: locks}
	case exit != 0:
		return fmt.Errorf("restic %s failed (exit %d)", c.Args[0], exit)
	}
	return nil
}

func (r Restic) output(ctx context.Context, args ...string) (process.Result, error) {
	return r.Runner.Output(ctx, process.Command{Name: "restic", Args: args, Env: r.Env})
}

func failure(name string, result process.Result) error {
	lines := strings.Split(strings.TrimSpace(string(result.Stderr)), "\n")
	if reason := strings.TrimSpace(lines[len(lines)-1]); reason != "" {
		return fmt.Errorf("restic %s failed (exit %d): %s", name, result.Exit, reason)
	}
	return fmt.Errorf("restic %s failed (exit %d)", name, result.Exit)
}
