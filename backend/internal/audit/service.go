package audit

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
)

type Service struct{ repo repository.AuditRepository }

func NewService(repo repository.AuditRepository) *Service { return &Service{repo: repo} }

func (s *Service) Record(ctx context.Context, actor *string, action, resourceType string, resourceID *string, metadata map[string]any, remoteAddr *string) error {
	return s.repo.Append(ctx, domain.AuditEvent{ID: newID(), ActorUserID: actor, Action: action, ResourceType: resourceType, ResourceID: resourceID, Metadata: metadata, RemoteAddr: remoteAddr, CreatedAt: time.Now().UTC()})
}

func (s *Service) List(ctx context.Context, limit, offset int) ([]domain.AuditEvent, error) {
	return s.repo.List(ctx, limit, offset)
}

func newID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand unavailable")
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}
