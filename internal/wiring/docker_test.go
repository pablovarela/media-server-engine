package wiring

import (
	"context"
	"encoding/binary"
	"net"
	"testing"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func multiplexed(t *testing.T, stdout, stderr string) client.ExecAttachResult {
	t.Helper()
	ours, theirs := net.Pipe()
	go func() {
		for stream, text := range map[byte]string{1: stdout, 2: stderr} {
			if text == "" {
				continue
			}
			header := make([]byte, 8)
			header[0] = stream
			binary.BigEndian.PutUint32(header[4:], uint32(len(text))) //nolint:gosec // test output is short
			_, _ = theirs.Write(append(header, text...))
		}
		_ = theirs.Close()
	}()
	return client.ExecAttachResult{HijackedResponse: client.NewHijackedResponse(ours, "")}
}

func TestExecReturnsTheCommandsOutput(t *testing.T) {
	ctx := context.Background()
	api := newMockDockerAPI(t)
	api.EXPECT().ExecCreate(ctx, "deluge", client.ExecCreateOptions{User: "abc", Cmd: []string{"python3", "-V"}, AttachStdout: true, AttachStderr: true}).Return(client.ExecCreateResult{ID: "e1"}, nil)
	api.EXPECT().ExecAttach(ctx, "e1", client.ExecAttachOptions{}).Return(multiplexed(t, "3.12\n", ""), nil)
	api.EXPECT().ExecInspect(ctx, "e1", client.ExecInspectOptions{}).Return(client.ExecInspectResult{ExitCode: 0}, nil)

	out, err := DockerClient{API: api}.Exec(ctx, "deluge", "abc", []string{"python3", "-V"})

	require.NoError(t, err)
	assert.Equal(t, "3.12\n", out)
}

func TestExecFailsWithTheEndOfTheCommandsErrors(t *testing.T) {
	ctx := context.Background()
	api := newMockDockerAPI(t)
	api.EXPECT().ExecCreate(ctx, "deluge", client.ExecCreateOptions{Cmd: []string{"sh", "-c", "false"}, AttachStdout: true, AttachStderr: true}).Return(client.ExecCreateResult{ID: "e1"}, nil)
	api.EXPECT().ExecAttach(ctx, "e1", client.ExecAttachOptions{}).Return(multiplexed(t, "", "sha256sum: WARNING: 1 computed checksum did NOT match\n"), nil)
	api.EXPECT().ExecInspect(ctx, "e1", client.ExecInspectOptions{}).Return(client.ExecInspectResult{ExitCode: 1}, nil)

	_, err := DockerClient{API: api}.Exec(ctx, "deluge", "", []string{"sh", "-c", "false"})

	assert.EqualError(t, err, "sha256sum: WARNING: 1 computed checksum did NOT match")
}
