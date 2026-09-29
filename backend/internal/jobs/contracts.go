package jobs

import (
	"context"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type Request struct {
	ResourceKey string
	Type        string
	ProjectID   *string
	RequestedBy *string
	Payload     map[string]any
}

type Handler interface {
	Type() string
	Run(ctx context.Context, job domain.Job) (map[string]any, error)
}

// RestartRecoverable opts into safe reconciliation after process interruption.
// The handler must inspect durable external state before repeating mutations.
type RestartRecoverable interface {
	RecoverInterrupted(ctx context.Context, job domain.Job) (retry bool, err error)
}

type JobRunner interface {
	Register(handler Handler) error
	Enqueue(ctx context.Context, request Request) (domain.Job, error)
	Cancel(ctx context.Context, jobID string) error
	Retry(ctx context.Context, jobID string) (domain.Job, error)
}
