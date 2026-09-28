package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
)

var ErrInvalidCredentials = errors.New("invalid credentials")
var ErrInactiveUser = errors.New("inactive user")
var ErrInvalidSession = errors.New("invalid session")

type Service struct {
	users    repository.UserRepository
	sessions repository.SessionRepository
	ttl      time.Duration
}

func NewService(users repository.UserRepository, sessions repository.SessionRepository, ttl time.Duration) *Service {
	return &Service{users: users, sessions: sessions, ttl: ttl}
}

func (s *Service) BootstrapAdmin(ctx context.Context, username, password string) error {
	if username == "" && password == "" {
		return nil
	}
	count, err := s.users.Count(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if password == "" {
		generated, genErr := randomToken(32)
		if genErr != nil {
			return fmt.Errorf("generate internal bootstrap secret: %w", genErr)
		}
		password = generated
	}
	if len(password) < 12 {
		return fmt.Errorf("bootstrap admin password must contain at least 12 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash bootstrap password: %w", err)
	}
	now := time.Now().UTC()
	return s.users.Create(ctx, domain.User{ID: newID(), Username: strings.TrimSpace(username), PasswordHash: string(hash), Role: domain.RoleAdmin, Active: true, CreatedAt: now, UpdatedAt: now})
}

func (s *Service) Login(ctx context.Context, username, password string) (domain.User, string, domain.Session, error) {
	user, err := s.users.ByUsername(ctx, strings.TrimSpace(username))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return domain.User{}, "", domain.Session{}, ErrInvalidCredentials
		}
		return domain.User{}, "", domain.Session{}, err
	}
	if !user.Active {
		return domain.User{}, "", domain.Session{}, ErrInactiveUser
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return domain.User{}, "", domain.Session{}, ErrInvalidCredentials
	}

	token, err := randomToken(32)
	if err != nil {
		return domain.User{}, "", domain.Session{}, err
	}
	now := time.Now().UTC()
	session := domain.Session{ID: newID(), UserID: user.ID, TokenHash: hashToken(token), ExpiresAt: now.Add(s.ttl), CreatedAt: now}
	if err := s.sessions.Create(ctx, session); err != nil {
		return domain.User{}, "", domain.Session{}, err
	}
	return user, token, session, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (domain.User, error) {
	if token == "" {
		return domain.User{}, ErrInvalidSession
	}
	session, err := s.sessions.ByTokenHash(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return domain.User{}, ErrInvalidSession
		}
		return domain.User{}, err
	}
	if !session.ExpiresAt.After(time.Now().UTC()) {
		_ = s.sessions.DeleteByTokenHash(ctx, hashToken(token))
		return domain.User{}, ErrInvalidSession
	}
	user, err := s.users.ByID(ctx, session.UserID)
	if err != nil {
		return domain.User{}, ErrInvalidSession
	}
	if !user.Active {
		return domain.User{}, ErrInactiveUser
	}
	return user, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.sessions.DeleteByTokenHash(ctx, hashToken(token))
}

func randomToken(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate secure token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func newID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand unavailable")
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Service) PasswordlessAdmin(ctx context.Context) (domain.User, error) {
	user, err := s.users.ByUsername(ctx, "admin")
	if err != nil {
		return domain.User{}, err
	}
	if !user.Active {
		return domain.User{}, ErrInactiveUser
	}
	return user, nil
}
