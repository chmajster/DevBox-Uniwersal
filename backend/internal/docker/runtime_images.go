package docker

import (
	"context"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"strings"
)

func (p *CLIProvider) RuntimeImages(ctx context.Context) ([]providers.RuntimeImage, error) {
	images, err := p.ListImages(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]providers.RuntimeImage, 0, len(images))
	for _, image := range images {
		repo := strings.TrimPrefix(image.Repository, "docker.io/")
		if !strings.Contains(repo, "/") {
			repo = "library/" + repo
		}
		out = append(out, providers.RuntimeImage{Reference: repo + ":" + image.Tag, ID: image.ID, Size: image.Size, Digest: image.Digest})
	}
	return out, nil
}
