package machine

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/moby/moby/client"
	"go.yaml.in/yaml/v3"
)

const (
	tcp             = "tcp"
	udp             = "udp"
	homepagePortVar = "${HOMEPAGE_PORT:-80}"
)

type Port struct {
	Number   int
	Protocol string
}

func (p Port) String() string {
	if p.Protocol == udp {
		return strconv.Itoa(p.Number) + "/udp"
	}
	return strconv.Itoa(p.Number)
}

type Published struct {
	Port      Port
	Container string
	Project   string
}

type PortsCheck struct {
	Ports     []Port
	Project   string
	Free      func(Port) bool
	Published func(ctx context.Context) ([]Published, error)
	Note      string
}

func StackPorts(compose []byte, homepagePort string) ([]Port, error) {
	if homepagePort == "" {
		homepagePort = "80"
	}
	var stack struct {
		Services yaml.Node `yaml:"services"`
	}
	if err := yaml.Unmarshal(compose, &stack); err != nil {
		return nil, err
	}
	var ports []Port
	for i := 1; i < len(stack.Services.Content); i += 2 {
		var service struct {
			Ports []string `yaml:"ports"`
		}
		if err := stack.Services.Content[i].Decode(&service); err != nil {
			return nil, err
		}
		for _, entry := range service.Ports {
			port, err := hostPort(strings.ReplaceAll(entry, homepagePortVar, homepagePort))
			if err != nil {
				return nil, err
			}
			ports = append(ports, port)
		}
	}
	return ports, nil
}

func hostPort(entry string) (Port, error) {
	mapping, protocol, found := strings.Cut(entry, "/")
	if !found {
		protocol = tcp
	}
	parts := strings.Split(mapping, ":")
	number, err := strconv.Atoi(parts[max(0, len(parts)-2)])
	if err != nil {
		return Port{}, fmt.Errorf("can't read the published port %q", entry)
	}
	return Port{Number: number, Protocol: protocol}, nil
}

func PortFree(p Port) bool {
	address := ":" + strconv.Itoa(p.Number)
	var config net.ListenConfig
	if p.Protocol == udp {
		conn, err := config.ListenPacket(context.Background(), udp, address)
		if err != nil {
			return false
		}
		return conn.Close() == nil
	}
	listener, err := config.Listen(context.Background(), tcp, address)
	if err != nil {
		return false
	}
	return listener.Close() == nil
}

func DockerPublished(ctx context.Context) ([]Published, error) {
	dockerCLI, err := command.NewDockerCli()
	if err != nil {
		return nil, err
	}
	if err := dockerCLI.Initialize(&flags.ClientOptions{}); err != nil {
		return nil, err
	}
	listed, err := dockerCLI.Client().ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		return nil, err
	}
	var published []Published
	for _, c := range listed.Items {
		name := strings.TrimPrefix(firstOr(c.Names, c.ID), "/")
		for _, p := range c.Ports {
			if p.PublicPort > 0 {
				published = append(published, Published{Port{int(p.PublicPort), p.Type}, name, c.Labels["com.docker.compose.project"]})
			}
		}
	}
	return published, nil
}

func firstOr(names []string, fallback string) string {
	if len(names) > 0 {
		return names[0]
	}
	return fallback
}

func portsResults(ctx context.Context, check PortsCheck) []Result {
	var busy []Port
	for _, p := range check.Ports {
		if !check.Free(p) {
			busy = append(busy, p)
		}
	}
	if len(busy) == 0 {
		return []Result{{Status: Pass, Line: freeLine(check)}}
	}
	published, err := check.Published(ctx)
	var results []Result
	for _, p := range busy {
		holder, ours := holderOf(p, published, check.Project)
		switch {
		case ours:
			continue
		case err != nil:
			results = append(results, Result{Status: Fail, Line: "port " + p.String() + " is in use (Docker couldn't say by what)", Fix: freeIt})
		default:
			results = append(results, Result{Status: Fail, Line: "port " + p.String() + " is in use by " + holder, Fix: freeIt})
		}
	}
	if len(results) == 0 {
		return []Result{{Status: Pass, Line: freeLine(check)}}
	}
	return results
}

const freeIt = "stop it, or free the port"

func holderOf(p Port, published []Published, project string) (holder string, ours bool) {
	for _, each := range published {
		if each.Port == p {
			return each.Container, each.Project == project
		}
	}
	return "something outside Docker", false
}

func freeLine(check PortsCheck) string {
	names := make([]string, 0, len(check.Ports))
	for _, p := range check.Ports {
		names = append(names, p.String())
	}
	line := "ports " + strings.Join(names, ", ") + " free"
	if check.Note != "" {
		line += " (" + check.Note + ")"
	}
	return line
}
