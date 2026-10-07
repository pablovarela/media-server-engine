package machine

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

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
	ports, unreadable, err := StackPorts([]byte(composeSnippet), nil, "")
	require.NoError(t, err)
	assert.Empty(t, unreadable)
	assert.Equal(t, []Port{{8096, "tcp"}, {7359, "udp"}, {80, "tcp"}}, ports)

	ports, _, err = StackPorts([]byte(composeSnippet), nil, "8080")
	require.NoError(t, err)
	assert.Contains(t, ports, Port{8080, "tcp"})
	assert.NotContains(t, ports, Port{80, "tcp"})
}

func TestStackPortsInEveryForm(t *testing.T) {
	compose := `services:
  a:
    ports:
      - "3000"
      - "127.0.0.1:8081:81"
      - "[::1]:8082:82"
      - "6881-6883:6881-6883/udp"
      - target: 84
        published: "8084"
        protocol: udp
      - target: 85
        published: 8085
      - target: 86
      - "nonsense:90"
`
	ports, unreadable, err := StackPorts([]byte(compose), nil, "")

	require.NoError(t, err)
	assert.Equal(t, []Port{{8081, "tcp"}, {8082, "tcp"}, {6881, "udp"}, {6882, "udp"}, {6883, "udp"}, {8084, "udp"}, {8085, "tcp"}}, ports)
	assert.Equal(t, []string{"nonsense:90"}, unreadable)
}

func TestTheOverrideAddsReplacesAndResetsPorts(t *testing.T) {
	override := `services:
  jellyfin:
    ports: !override
      - "8097:8096"
  homepage:
    ports: !reset []
  extra:
    ports:
      - "9443:443"
`
	ports, unreadable, err := StackPorts([]byte(composeSnippet), []byte(override), "")

	require.NoError(t, err)
	assert.Empty(t, unreadable)
	assert.Equal(t, []Port{{8097, "tcp"}, {9443, "tcp"}}, ports)
}

func TestTheOverrideAddsToAServicesPorts(t *testing.T) {
	override := "services:\n  jellyfin:\n    ports:\n      - \"8443:8920\"\n      - \"8096:8096\"\n"

	ports, _, err := StackPorts([]byte(composeSnippet), []byte(override), "")

	require.NoError(t, err)
	assert.Equal(t, []Port{{8096, "tcp"}, {7359, "udp"}, {8443, "tcp"}, {80, "tcp"}}, ports)
}

func TestAHomepagePortThatIsNotANumberIsReported(t *testing.T) {
	_, unreadable, err := StackPorts([]byte(composeSnippet), nil, "eighty")

	require.NoError(t, err)
	assert.Equal(t, []string{"eighty:3000"}, unreadable)
}

func TestStackPortsOfTheEnginesComposeFile(t *testing.T) {
	compose, err := os.ReadFile("../../docker-compose.yml")
	require.NoError(t, err)

	ports, unreadable, err := StackPorts(compose, nil, "")

	require.NoError(t, err)
	assert.Empty(t, unreadable)
	assert.Equal(t, []Port{
		{8096, "tcp"}, {8920, "tcp"}, {7359, "udp"}, {1900, "udp"}, {8112, "tcp"}, {58846, "tcp"}, {58946, "tcp"},
		{6881, "tcp"}, {6881, "udp"}, {9696, "tcp"}, {8198, "tcp"}, {8989, "tcp"}, {7878, "tcp"}, {6767, "tcp"},
		{9000, "tcp"}, {5055, "tcp"}, {6246, "tcp"}, {80, "tcp"},
	}, ports)
}

func TestAnUnreadablePortIsAProblem(t *testing.T) {
	f := newPortsFixture(t)
	f.env.Ports.Unreadable = []string{"eighty:3000"}

	assert.Contains(t, f.render(t), "  the stack's ports can be read... ✗ \"eighty:3000\"\n      run: correct it in compose.override.yml, or HOMEPAGE_PORT in installation.env\n")
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

	assert.Contains(t, out, "  the user manager has the docker group... ✓\n  ports... ✓ 8096, 7359/udp, 80 free\n")
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

	assert.Contains(t, out, "  ports... ✓ 8096, 7359/udp, 80 free\n")
}

func TestBusyPortsAreNamed(t *testing.T) {
	f := newPortsFixture(t)
	f.busy = map[Port]bool{{8096, "tcp"}: true, {80, "tcp"}: true}
	f.published = []Published{{Port{8096, "tcp"}, "other-jellyfin", "other"}}

	out := f.render(t)

	assert.Contains(t, out, "  ports... ✗ 8096 is in use by other-jellyfin; 80 is in use by something outside Docker\n      run: stop them, or free the ports\n")
	assert.Contains(t, out, "1 thing to fix.")
}

func TestBusyPortsWhenDockerCannotSay(t *testing.T) {
	f := newPortsFixture(t)
	f.busy = map[Port]bool{{7359, "udp"}: true}
	f.err = errors.New("permission denied")

	assert.Contains(t, f.render(t), "  ports... ? 7359/udp in use, and Docker couldn't say by what (permission denied)\n")
}

func TestBusyPortsWhenDockerRunsOutOfTime(t *testing.T) {
	f := newPortsFixture(t)
	f.busy = map[Port]bool{{8096, "tcp"}: true, {80, "tcp"}: true}
	f.env.Timeout = 20 * time.Millisecond
	f.env.Ports.Published = func(ctx context.Context) ([]Published, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	out := f.render(t)

	assert.Contains(t, out, "  ports... ? 8096, 80 in use, and Docker couldn't say by what (no answer within 20ms)\n")
	assert.NotContains(t, out, "✗")
}

func TestPortsWaitForDocker(t *testing.T) {
	f := newPortsFixture(t)
	f.fails("docker info")

	assert.Contains(t, f.render(t), "  ports... – skipped until Docker answers\n")
}

func TestPortsWaitingOnAnUncheckedDockerSaySo(t *testing.T) {
	f := newPortsFixture(t)
	stalling(t, f.fixture, "docker info")

	assert.Contains(t, f.render(t), "  ports... – skipped: Docker answers couldn't be checked\n")
}
