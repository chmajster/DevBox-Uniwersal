package jobs

import (
	"context"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type Request struct {
	Type          string
	ProjectID     *string
	ApplicationID *string
	RequestedBy   *string
	Payload       map[string]any
}

type Handler interface {
	Type() string
	Run(ctx context.Context, job domain.Job) (map[string]any, error)
}

type JobRunner interface {
	Register(handler Handler) error
	Enqueue(ctx context.Context, request Request) (domain.Job, error)
	Cancel(ctx context.Context, jobID string) error
	Retry(ctx context.Context, jobID string) (domain.Job, error)
}
