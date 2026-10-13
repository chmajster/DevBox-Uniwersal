package docker

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

func (p *CLIProvider) LogStreams(ctx context.Context, id string, tail int) ([]providers.ContainerLogLine, error) {
	if err := validateContainerRef(id); err != nil {
		return nil, err
	}
	if tail < 1 {
		tail = 200
	}
	if tail > 2000 {
		tail = 2000
	}
	stdout, stderr, err := p.runner.Run(ctx, "container", "logs", "--timestamps", "--tail", strconv.Itoa(tail), id)
	if err != nil {
		return nil, err
	}
	out := []providers.ContainerLogLine{}
	for _, stream := range []struct {
		name string
		data []byte
	}{{"stdout", stdout}, {"stderr", stderr}} {
		for _, line := range strings.Split(strings.TrimSuffix(string(stream.data), "\n"), "\n") {
			if line != "" {
				out = append(out, providers.ContainerLogLine{Stream: stream.name, Line: line})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, _, _ := strings.Cut(out[i].Line, " ")
		b, _, _ := strings.Cut(out[j].Line, " ")
		at, ae := time.Parse(time.RFC3339Nano, a)
		bt, be := time.Parse(time.RFC3339Nano, b)
		return ae == nil && be == nil && at.Before(bt)
	})
	return out, nil
}
