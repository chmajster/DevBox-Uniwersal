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
var ErrInvalidUserInput = errors.New("invalid user input")
var ErrUsernameExists = errors.New("username already exists")
var ErrLastActiveAdmin = errors.New("at least one active admin account is required")

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

func (s *Service) ListUsers(ctx context.Context) ([]domain.User, error) {
	return s.users.List(ctx)
}

func (s *Service) CreateUser(ctx context.Context, username string, requestedPassword *string, role domain.Role, active bool) (domain.User, string, error) {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 120 {
		return domain.User{}, "", fmt.Errorf("%w: username is required and must be at most 120 characters", ErrInvalidUserInput)
	}
	if !role.Valid() {
		return domain.User{}, "", fmt.Errorf("%w: role must be admin, operator or viewer", ErrInvalidUserInput)
	}
	if _, err := s.users.ByUsername(ctx, username); err == nil {
		return domain.User{}, "", ErrUsernameExists
	} else if !errors.Is(err, repository.ErrNotFound) {
		return domain.User{}, "", err
	}
	password, err := userPassword(requestedPassword)
	if err != nil {
		return domain.User{}, "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return domain.User{}, "", fmt.Errorf("hash user password: %w", err)
	}
	now := time.Now().UTC()
	user := domain.User{
		ID: newID(), Username: username, PasswordHash: string(hash), Role: role, Active: active,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.users.Create(ctx, user); err != nil {
		return domain.User{}, "", err
	}
	return user, password, nil
}

func (s *Service) UpdateUserAccess(ctx context.Context, id string, role domain.Role, active bool) (domain.User, error) {
	if !role.Valid() {
		return domain.User{}, fmt.Errorf("%w: role must be admin, operator or viewer", ErrInvalidUserInput)
	}
	current, err := s.users.ByID(ctx, id)
	if err != nil {
		return domain.User{}, err
	}
	if current.Role == domain.RoleAdmin && current.Active && (role != domain.RoleAdmin || !active) {
		count, err := s.users.CountActiveAdmins(ctx)
		if err != nil {
			return domain.User{}, err
		}
		if count <= 1 {
			return domain.User{}, ErrLastActiveAdmin
		}
	}
	if current.Role == role && current.Active == active {
		return current, nil
	}
	if err := s.users.UpdateRoleAndActive(ctx, id, role, active, time.Now().UTC()); err != nil {
		return domain.User{}, err
	}
	if err := s.sessions.DeleteByUserID(ctx, id); err != nil {
		return domain.User{}, fmt.Errorf("revoke sessions after user update: %w", err)
	}
	return s.users.ByID(ctx, id)
}

func (s *Service) ChangeUserPassword(ctx context.Context, id string, requestedPassword *string) (string, error) {
	if _, err := s.users.ByID(ctx, id); err != nil {
		return "", err
	}
	password, err := userPassword(requestedPassword)
	if err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash user password: %w", err)
	}
	if err := s.users.UpdatePasswordHash(ctx, id, string(hash), time.Now().UTC()); err != nil {
		return "", err
	}
	if err := s.sessions.DeleteByUserID(ctx, id); err != nil {
		return "", fmt.Errorf("revoke sessions after password change: %w", err)
	}
	return password, nil
}

func (s *Service) RevokeUserSessions(ctx context.Context, id string) error {
	if _, err := s.users.ByID(ctx, id); err != nil {
		return err
	}
	return s.sessions.DeleteByUserID(ctx, id)
}

func (s *Service) DeleteUser(ctx context.Context, id string) error {
	current, err := s.users.ByID(ctx, id)
	if err != nil {
		return err
	}
	if current.Role == domain.RoleAdmin && current.Active {
		count, err := s.users.CountActiveAdmins(ctx)
		if err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastActiveAdmin
		}
	}
	return s.users.Delete(ctx, id)
}

func userPassword(requested *string) (string, error) {
	if requested == nil {
		generated, err := randomToken(24)
		if err != nil {
			return "", fmt.Errorf("generate user password: %w", err)
		}
		return generated, nil
	}
	password := *requested
	if len(password) < 12 {
		return "", fmt.Errorf("%w: password must contain at least 12 characters", ErrInvalidUserInput)
	}
	if len(password) > 1024 {
		return "", fmt.Errorf("%w: password is too long", ErrInvalidUserInput)
	}
	return password, nil
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
