package machine

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const composeSnippet = `services:
  jellyfin:
    ports:
      - "8096:8096"        # HTTP
      - "7359:7359/udp"
  sonarr:
    environment:
      - TZ=UTC
  homepage:
    ports:
      - "${HOMEPAGE_PORT:-80}:3000"
`

func TestStackPorts(t *testing.T) {
	ports, err := StackPorts([]byte(composeSnippet), "")
	require.NoError(t, err)
	assert.Equal(t, []Port{{8096, "tcp"}, {7359, "udp"}, {80, "tcp"}}, ports)

	ports, err = StackPorts([]byte(composeSnippet), "8080")
	require.NoError(t, err)
	assert.Contains(t, ports, Port{8080, "tcp"})
	assert.NotContains(t, ports, Port{80, "tcp"})
}

func TestStackPortsOfTheEnginesComposeFile(t *testing.T) {
	compose, err := os.ReadFile("../../docker-compose.yml")
	require.NoError(t, err)

	ports, err := StackPorts(compose, "")

	require.NoError(t, err)
	for _, want := range []Port{{8096, "tcp"}, {8989, "tcp"}, {7878, "tcp"}, {9000, "tcp"}, {80, "tcp"}, {7359, "udp"}} {
		assert.Contains(t, ports, want)
	}
}

func TestOnlyAnAddressInUseMakesAPortBusy(t *testing.T) {
	assert.True(t, freeAfter(nil))
	assert.False(t, freeAfter(&net.OpError{Op: "listen", Err: os.NewSyscallError("bind", syscall.EADDRINUSE)}))
	assert.True(t, freeAfter(&net.OpError{Op: "listen", Err: os.NewSyscallError("bind", syscall.EACCES)}))
}

type portsFixture struct {
	*fixture
	busy      map[Port]bool
	published []Published
	err       error
}

func newPortsFixture(t *testing.T) *portsFixture {
	f := &portsFixture{fixture: newFixture(t), busy: map[Port]bool{}}
	f.env.Ports = PortsCheck{
		Ports:     []Port{{8096, "tcp"}, {7359, "udp"}, {80, "tcp"}},
		Project:   "media-server",
		Free:      func(p Port) bool { return !f.busy[p] },
		Published: func(context.Context) ([]Published, error) { return f.published, f.err },
	}
	return f
}

func TestFreePorts(t *testing.T) {
	f := newPortsFixture(t)

	out := f.render(t)

	assert.Contains(t, out, "  ✓ the user manager has the docker group\n  ✓ ports 8096, 7359/udp, 80 free\n")
	assert.Contains(t, out, "This machine is ready.")
}

func TestPortsHeldByThisStackCountAsFree(t *testing.T) {
	f := newPortsFixture(t)
	f.busy = map[Port]bool{{8096, "tcp"}: true, {7359, "udp"}: true, {80, "tcp"}: true}
	f.published = []Published{
		{Port{8096, "tcp"}, "jellyfin", "media-server"},
		{Port{7359, "udp"}, "jellyfin", "media-server"},
		{Port{80, "tcp"}, "homepage", "media-server"},
	}

	out := f.render(t)

	assert.Contains(t, out, "  ✓ ports 8096, 7359/udp, 80 free\n")
}

func TestBusyPortsAreNamed(t *testing.T) {
	f := newPortsFixture(t)
	f.busy = map[Port]bool{{8096, "tcp"}: true, {80, "tcp"}: true}
	f.published = []Published{{Port{8096, "tcp"}, "other-jellyfin", "other"}}

	out := f.render(t)

	assert.Contains(t, out, "  ✗ port 8096 is in use by other-jellyfin\n      run: stop it, or free the port\n")
	assert.Contains(t, out, "  ✗ port 80 is in use by something outside Docker\n      run: stop it, or free the port\n")
	assert.Contains(t, out, "2 things to fix.")
}

func TestBusyPortsWhenDockerCannotSay(t *testing.T) {
	f := newPortsFixture(t)
	f.busy = map[Port]bool{{7359, "udp"}: true}
	f.err = errors.New("permission denied")

	assert.Contains(t, f.render(t), "  ✗ port 7359/udp is in use (Docker couldn't say by what)\n")
}

func TestPortsWaitForDocker(t *testing.T) {
	f := newPortsFixture(t)
	f.fails("docker info")

	assert.Contains(t, f.render(t), "  – ports: skipped until Docker answers\n")
}

func TestThePortsNote(t *testing.T) {
	f := newPortsFixture(t)
	f.env.Ports.Note = "several installations here; using the default homepage port 80"

	out := f.render(t)

	assert.True(t, strings.Contains(out, "  ✓ ports 8096, 7359/udp, 80 free (several installations here; using the default homepage port 80)\n"), out)
}
