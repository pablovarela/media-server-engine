package wiring

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

const stderrLimit = 300

type dockerAPI interface {
	ExecCreate(ctx context.Context, container string, options client.ExecCreateOptions) (client.ExecCreateResult, error)
	ExecAttach(ctx context.Context, execID string, options client.ExecAttachOptions) (client.ExecAttachResult, error)
	ExecInspect(ctx context.Context, execID string, options client.ExecInspectOptions) (client.ExecInspectResult, error)
	ContainerStop(ctx context.Context, container string, options client.ContainerStopOptions) (client.ContainerStopResult, error)
	ContainerStart(ctx context.Context, container string, options client.ContainerStartOptions) (client.ContainerStartResult, error)
}

type DockerClient struct {
	API dockerAPI
}

func (d DockerClient) Exec(ctx context.Context, container, user string, command []string) (string, error) {
	created, err := d.API.ExecCreate(ctx, container, client.ExecCreateOptions{User: user, Cmd: command, AttachStdout: true, AttachStderr: true})
	if err != nil {
		return "", err
	}
	attached, err := d.API.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return "", err
	}
	defer attached.Close()
	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, attached.Reader); err != nil {
		return "", err
	}
	inspected, err := d.API.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return "", err
	}
	if inspected.ExitCode != 0 {
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > stderrLimit {
			detail = detail[len(detail)-stderrLimit:]
		}
		if detail == "" {
			detail = fmt.Sprintf("exit %d", inspected.ExitCode)
		}
		return stdout.String(), errors.New(detail)
	}
	return stdout.String(), nil
}

func (d DockerClient) Stop(ctx context.Context, container string) error {
	_, err := d.API.ContainerStop(ctx, container, client.ContainerStopOptions{})
	return err
}

func (d DockerClient) Start(ctx context.Context, container string) error {
	_, err := d.API.ContainerStart(ctx, container, client.ContainerStartOptions{})
	return err
}
