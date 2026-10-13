package docker

import (
	"context"
	"regexp"
	"strconv"
)

var publishedTCPPort = regexp.MustCompile(`:([0-9]+)->[0-9]+/tcp`)

// Docker can reserve ports through NAT without a listening userspace socket.
func (p *CLIProvider) PublishedApplicationPorts(ctx context.Context) (map[int]bool, error) {
	containers, err := p.ListContainers(ctx)
	if err != nil {
		return nil, err
	}
	ports := map[int]bool{}
	for _, container := range containers {
		for _, match := range publishedTCPPort.FindAllStringSubmatch(container.Ports, -1) {
			port, _ := strconv.Atoi(match[1])
			ports[port] = true
		}
	}
	return ports, nil
}
