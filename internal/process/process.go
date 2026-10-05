package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

type Command struct {
	Name       string
	Args       []string
	Dir        string
	Env        []string
	ExtraFiles []*os.File
}

type Result struct {
	Stdout []byte
	Stderr []byte
	Exit   int
}

type System struct {
	Out    io.Writer
	ErrOut io.Writer
}

func (s System) Run(ctx context.Context, c Command) (int, error) {
	command := prepared(ctx, c)
	command.Stdout = s.Out
	command.Stderr = s.ErrOut
	return finished(ctx, c, command.Run())
}

func (s System) Output(ctx context.Context, c Command) (Result, error) {
	var stdout, stderr bytes.Buffer
	command := prepared(ctx, c)
	command.Stdout = &stdout
	command.Stderr = &stderr
	exit, err := finished(ctx, c, command.Run())
	return Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), Exit: exit}, err
}

func prepared(ctx context.Context, c Command) *exec.Cmd {
	command := exec.CommandContext(ctx, c.Name, c.Args...) //nolint:gosec // runs the programs the engine names, with arguments it builds
	command.Dir = c.Dir
	command.Env = append(os.Environ(), c.Env...)
	command.ExtraFiles = c.ExtraFiles
	command.Cancel = func() error { return command.Process.Signal(os.Interrupt) }
	command.WaitDelay = time.Minute
	return command
}

func finished(ctx context.Context, c Command, err error) (int, error) {
	if ctx.Err() != nil {
		return -1, fmt.Errorf("%s was interrupted", c.Name)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}
