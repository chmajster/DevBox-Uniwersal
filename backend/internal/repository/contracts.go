package repository

import (
	"context"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type UserRepository interface {
	Count(context.Context) (int, error)
	Create(context.Context, domain.User) error
	ByID(context.Context, string) (domain.User, error)
	ByUsername(context.Context, string) (domain.User, error)
}

type SessionRepository interface {
	Create(context.Context, domain.Session) error
	ByTokenHash(context.Context, string) (domain.Session, error)
	DeleteByTokenHash(context.Context, string) error
	DeleteExpired(context.Context, time.Time) error
}

type JobRepository interface {
	List(context.Context, int, int) ([]domain.Job, error)
	ByID(context.Context, string) (domain.Job, error)
}

type AuditRepository interface {
	Append(context.Context, domain.AuditEvent) error
	List(context.Context, int, int) ([]domain.AuditEvent, error)
}
