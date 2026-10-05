package machine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"syscall"

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
	Ports      []Port
	Project    string
	Free       func(Port) bool
	Published  func(ctx context.Context) ([]Published, error)
	Note       string
	Unreadable []string
}

func StackPorts(engine, override []byte, homepagePort string) (ports []Port, unreadable []string, err error) {
	if homepagePort == "" {
		homepagePort = "80"
	}
	services, err := servicePorts(engine)
	if err != nil {
		return nil, nil, err
	}
	overridden, err := servicePorts(override)
	if err != nil {
		return nil, nil, err
	}
	for _, each := range overridden {
		services = merged(services, each)
	}
	for _, service := range services {
		for _, entry := range service.entries {
			published, protocol, text := portEntry(entry, homepagePort)
			found, ok := hostPorts(published, protocol)
			if !ok {
				unreadable = append(unreadable, text)
			}
			for _, port := range found {
				if !slices.Contains(ports, port) {
					ports = append(ports, port)
				}
			}
		}
	}
	return ports, unreadable, nil
}

type serviceEntries struct {
	name    string
	entries []yaml.Node
	replace bool
}

func servicePorts(compose []byte) ([]serviceEntries, error) {
	var stack struct {
		Services yaml.Node `yaml:"services"`
	}
	if err := yaml.Unmarshal(compose, &stack); err != nil {
		return nil, err
	}
	var services []serviceEntries
	for i := 1; i < len(stack.Services.Content); i += 2 {
		var service struct {
			Ports yaml.Node `yaml:"ports"`
		}
		if err := stack.Services.Content[i].Decode(&service); err != nil {
			return nil, err
		}
		replace := service.Ports.Tag == "!override" || service.Ports.Tag == "!reset"
		var entries []yaml.Node
		for _, entry := range service.Ports.Content {
			entries = append(entries, *entry)
		}
		services = append(services, serviceEntries{name: stack.Services.Content[i-1].Value, entries: entries, replace: replace})
	}
	return services, nil
}

func merged(services []serviceEntries, override serviceEntries) []serviceEntries {
	for i, service := range services {
		if service.name != override.name {
			continue
		}
		if override.replace {
			services[i].entries = override.entries
		} else {
			services[i].entries = append(services[i].entries, override.entries...)
		}
		return services
	}
	return append(services, override)
}

func portEntry(entry yaml.Node, homepagePort string) (published, protocol, text string) {
	if entry.Kind == yaml.MappingNode {
		var long struct {
			Published string `yaml:"published"`
			Protocol  string `yaml:"protocol"`
		}
		_ = entry.Decode(&long)
		return long.Published, orTCP(long.Protocol), long.Published
	}
	text = strings.ReplaceAll(entry.Value, homepagePortVar, homepagePort)
	mapping, protocol, _ := strings.Cut(text, "/")
	if strings.HasPrefix(mapping, "[") {
		if _, rest, found := strings.Cut(mapping, "]:"); found {
			mapping = rest
		}
	}
	parts := strings.Split(mapping, ":")
	if len(parts) < 2 {
		return "", orTCP(protocol), text
	}
	return parts[len(parts)-2], orTCP(protocol), text
}

func orTCP(protocol string) string {
	if protocol == "" {
		return tcp
	}
	return protocol
}

func hostPorts(published, protocol string) ([]Port, bool) {
	if published == "" {
		return nil, true
	}
	first, last, ranged := strings.Cut(published, "-")
	if !ranged {
		last = first
	}
	from, err := strconv.Atoi(first)
	to, errLast := strconv.Atoi(last)
	if err != nil || errLast != nil || from < 1 || to < from || to > 65535 {
		return nil, false
	}
	ports := make([]Port, 0, to-from+1)
	for number := from; number <= to; number++ {
		ports = append(ports, Port{Number: number, Protocol: protocol})
	}
	return ports, true
}

func PortFree(p Port) bool {
	address := ":" + strconv.Itoa(p.Number)
	var config net.ListenConfig
	if p.Protocol == udp {
		conn, err := config.ListenPacket(context.Background(), udp, address)
		if err == nil {
			err = conn.Close()
		}
		return freeAfter(err)
	}
	listener, err := config.Listen(context.Background(), tcp, address)
	if err == nil {
		err = listener.Close()
	}
	return freeAfter(err)
}

func freeAfter(listening error) bool {
	return !errors.Is(listening, syscall.EADDRINUSE)
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
	unreadable := make([]Result, 0, len(check.Unreadable))
	for _, entry := range check.Unreadable {
		unreadable = append(unreadable, Result{Status: Fail, Line: fmt.Sprintf("the stack's port %q can't be read", entry), Fix: "correct it in compose.override.yml, or HOMEPAGE_PORT in installation.env"})
	}
	if len(check.Ports) == 0 {
		return unreadable
	}
	return append(unreadable, busyResults(ctx, check)...)
}

func busyResults(ctx context.Context, check PortsCheck) []Result {
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
