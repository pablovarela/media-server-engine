package restic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
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
	Binary string
	Runner Runner
	Env    []string
	Log    io.Writer
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
	process := "?"
	if l.PID != 0 {
		process = strconv.Itoa(l.PID)
	}
	return fmt.Sprintf("%s lock from %s (process %s) since %s", kind, host, process, l.Time.Format("2006-01-02 15:04"))
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
	message.WriteString("\nEach of those locks is still being refreshed, so restic is running on that machine: let it finish, or stop it there, then run this again.")
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
func (r Restic) Check(ctx context.Context) error {
	return r.run(ctx, process.Command{Args: []string{"check", retryLock, waitForLocks}})
}

func (r Restic) Backup(ctx context.Context, o BackupOptions) (BackupSummary, error) {
	args := []string{"backup", "--json", retryLock, waitForLocks, "--host", o.Host}
	for _, tag := range o.Tags {
		args = append(args, "--tag", tag)
	}
	args = append(args, "--exclude-file", o.ExcludeFile)
	messages, problems := &backupMessages{log: r.Log}, &backupMessages{log: r.Log}
	err := r.run(ctx, process.Command{Args: append(args, o.Paths...), Dir: o.Dir, ExtraFiles: o.Inherit, Stdout: messages, Stderr: problems})
	return messages.summary, err
}

func (r Restic) Forget(ctx context.Context, host string, inherit []*os.File) (ForgetSummary, error) {
	var listed bytes.Buffer
	err := r.run(ctx, process.Command{ExtraFiles: inherit, Stdout: &listed, Args: []string{
		"forget", "--json", retryLock, waitForLocks, "--host", host, "--keep-daily", "7", "--keep-weekly", "4", "--keep-monthly", "6",
	}})
	if err != nil {
		return ForgetSummary{}, err
	}
	var groups []struct {
		Keep   []Snapshot `json:"keep"`
		Remove []Snapshot `json:"remove"`
	}
	if err := json.Unmarshal(listed.Bytes(), &groups); err != nil && len(bytes.TrimSpace(listed.Bytes())) > 0 {
		return ForgetSummary{}, fmt.Errorf("restic forget printed unreadable JSON: %w", err)
	}
	var summary ForgetSummary
	for _, group := range groups {
		summary.Kept += len(group.Keep)
		for _, removed := range group.Remove {
			summary.Removed = append(summary.Removed, removed.Time)
		}
	}
	if r.Log != nil {
		_, _ = fmt.Fprintln(r.Log, summary.String())
	}
	return summary, nil
}

func (r Restic) Prune(ctx context.Context, inherit []*os.File) error {
	return r.run(ctx, process.Command{ExtraFiles: inherit, Args: []string{"prune", retryLock, waitForLocks}})
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
	result, err := r.output(ctx, "cat", "config", "--no-lock")
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
	c.Name, c.Env = r.Binary, r.Env
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
	return r.Runner.Output(ctx, process.Command{Name: r.Binary, Args: args, Env: r.Env})
}

func failure(name string, result process.Result) error {
	lines := strings.Split(strings.TrimSpace(string(result.Stderr)), "\n")
	if reason := strings.TrimSpace(lines[len(lines)-1]); reason != "" {
		return fmt.Errorf("restic %s failed (exit %d): %s", name, result.Exit, reason)
	}
	return fmt.Errorf("restic %s failed (exit %d)", name, result.Exit)
}

type BackupSummary struct {
	SnapshotID      string `json:"snapshot_id"`
	FilesNew        int    `json:"files_new"`
	FilesChanged    int    `json:"files_changed"`
	FilesUnmodified int    `json:"files_unmodified"`
	DataAdded       int64  `json:"data_added"`
	DataAddedPacked int64  `json:"data_added_packed"`
}

func (s BackupSummary) String() string {
	id := s.SnapshotID
	if len(id) > 8 {
		id = id[:8]
	}
	return fmt.Sprintf("snapshot %s: %d new, %d changed, %d unchanged files; %s added (%s stored)", id, s.FilesNew, s.FilesChanged, s.FilesUnmodified, size(s.DataAdded), size(s.DataAddedPacked))
}

func size(bytes int64) string {
	value, units := float64(bytes), []string{"B", "KiB", "MiB", "GiB", "TiB"}
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d B", bytes)
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

type ForgetSummary struct {
	Kept    int
	Removed []time.Time
}

func (s ForgetSummary) String() string {
	text := fmt.Sprintf("kept %d, removed %d", s.Kept, len(s.Removed))
	if len(s.Removed) > 0 {
		times := make([]string, len(s.Removed))
		for n, at := range s.Removed {
			times[n] = at.Format("2006-01-02 15:04")
		}
		text += " (" + strings.Join(times, ", ") + ")"
	}
	return text
}

type backupMessages struct {
	log     io.Writer
	summary BackupSummary
	pending strings.Builder
}

func (m *backupMessages) Write(b []byte) (int, error) {
	m.pending.Write(b)
	text := m.pending.String()
	for {
		line, rest, found := strings.Cut(text, "\n")
		if !found {
			break
		}
		m.read(line)
		text = rest
	}
	m.pending.Reset()
	m.pending.WriteString(text)
	return len(b), nil
}

func (m *backupMessages) read(line string) {
	var message struct {
		Type    string `json:"message_type"`
		During  string `json:"during"`
		Item    string `json:"item"`
		Action  string `json:"action"`
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(line), &message) != nil {
		m.say(line)
		return
	}
	switch message.Type {
	case "status":
	case "summary":
		_ = json.Unmarshal([]byte(line), &m.summary)
		m.say(m.summary.String())
	case "error":
		m.say(fmt.Sprintf("error during %s: %s: %s", message.During, message.Item, message.Error.Message))
	case "exit_error":
		m.say(message.Message)
	case "verbose_status":
		m.say(message.Action + " " + message.Item)
	default:
		m.say(line)
	}
}

func (m *backupMessages) say(line string) {
	if m.log != nil {
		_, _ = fmt.Fprintln(m.log, line)
	}
}
