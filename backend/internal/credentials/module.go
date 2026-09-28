package credentials

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

var (
	ErrNotFound     = errors.New("credential not found")
	ErrInvalidInput = errors.New("invalid credential input")
	ErrInUse        = errors.New("credential is in use")
)

type Credential struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	HasSecret bool      `json:"has_secret"`
	CreatedBy *string   `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type createInput struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Secret string `json:"secret"`
}

type updateInput struct {
	Name   *string `json:"name"`
	Secret *string `json:"secret"`
}

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) List(ctx context.Context) ([]Credential, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,kind,created_by,created_at,updated_at FROM credentials ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	defer rows.Close()
	out := []Credential{}
	for rows.Next() {
		item, err := scanCredential(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id string) (Credential, error) {
	item, err := scanCredential(r.db.QueryRowContext(ctx, `SELECT id,name,kind,created_by,created_at,updated_at FROM credentials WHERE id=?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, ErrNotFound
	}
	return item, err
}

func (r *Repository) Create(ctx context.Context, item Credential, scope, secretName string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO credentials(id,name,kind,secret_scope,secret_name,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		item.ID, item.Name, item.Kind, scope, secretName, item.CreatedBy, item.CreatedAt.UTC().Format(time.RFC3339Nano), item.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("create credential: %w", err)
	}
	return nil
}

func (r *Repository) UpdateName(ctx context.Context, id, name string, when time.Time) error {
	res, err := r.db.ExecContext(ctx, `UPDATE credentials SET name=?,updated_at=? WHERE id=?`, name, when.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("update credential: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) SecretRef(ctx context.Context, id string) (kind, scope, name string, err error) {
	err = r.db.QueryRowContext(ctx, `SELECT kind,secret_scope,secret_name FROM credentials WHERE id=?`, id).Scan(&kind, &scope, &name)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return
}

func (r *Repository) Touch(ctx context.Context, id string, when time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE credentials SET updated_at=? WHERE id=?`, when.UTC().Format(time.RFC3339Nano), id)
	return err
}

func (r *Repository) CountUsage(ctx context.Context, id string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM project_sources WHERE credential_secret_id=?) + (SELECT COUNT(*) FROM source_control_integrations WHERE credential_id=?)`, id, id).Scan(&count)
	return count, err
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM credentials WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

type scanFn func(dest ...any) error

func scanCredential(scan scanFn) (Credential, error) {
	var item Credential
	var createdBy sql.NullString
	var created, updated string
	if err := scan(&item.ID, &item.Name, &item.Kind, &createdBy, &created, &updated); err != nil {
		return Credential{}, err
	}
	if createdBy.Valid {
		item.CreatedBy = &createdBy.String
	}
	var err error
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return Credential{}, err
	}
	item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return Credential{}, err
	}
	item.HasSecret = true
	return item, nil
}

type Service struct {
	repo    *Repository
	secrets secrets.SecretStore
}

func NewService(repo *Repository, store secrets.SecretStore) *Service {
	return &Service{repo: repo, secrets: store}
}

func (s *Service) List(ctx context.Context) ([]Credential, error) { return s.repo.List(ctx) }

func (s *Service) Create(ctx context.Context, input createInput, actor *string) (Credential, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Kind = strings.TrimSpace(input.Kind)
	if input.Name == "" || len(input.Name) > 120 {
		return Credential{}, fmt.Errorf("%w: name is required and must be at most 120 characters", ErrInvalidInput)
	}
	if input.Kind != "token" && input.Kind != "ssh_key" {
		return Credential{}, fmt.Errorf("%w: kind must be token or ssh_key", ErrInvalidInput)
	}
	if strings.TrimSpace(input.Secret) == "" {
		return Credential{}, fmt.Errorf("%w: secret is required", ErrInvalidInput)
	}
	if s.secrets == nil {
		return Credential{}, errors.New("provider unavailable: secret store is not configured")
	}

	id := newID()
	scope := "credential/" + id
	secretName := "value"
	if err := s.secrets.Put(ctx, scope, secretName, []byte(input.Secret)); err != nil {
		return Credential{}, fmt.Errorf("store credential secret: %w", err)
	}
	now := time.Now().UTC()
	item := Credential{ID: id, Name: input.Name, Kind: input.Kind, HasSecret: true, CreatedBy: actor, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.Create(ctx, item, scope, secretName); err != nil {
		_ = s.secrets.Delete(ctx, scope, secretName)
		return Credential{}, err
	}
	return item, nil
}

func (s *Service) Update(ctx context.Context, id string, input updateInput) (Credential, error) {
	current, err := s.repo.Get(ctx, id)
	if err != nil {
		return Credential{}, err
	}
	when := time.Now().UTC()
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" || len(name) > 120 {
			return Credential{}, fmt.Errorf("%w: invalid name", ErrInvalidInput)
		}
		if err := s.repo.UpdateName(ctx, id, name, when); err != nil {
			return Credential{}, err
		}
	}
	if input.Secret != nil {
		if strings.TrimSpace(*input.Secret) == "" {
			return Credential{}, fmt.Errorf("%w: secret cannot be empty", ErrInvalidInput)
		}
		if s.secrets == nil {
			return Credential{}, errors.New("provider unavailable: secret store is not configured")
		}
		_, scope, name, err := s.repo.SecretRef(ctx, id)
		if err != nil {
			return Credential{}, err
		}
		if err := s.secrets.Put(ctx, scope, name, []byte(*input.Secret)); err != nil {
			return Credential{}, err
		}
		if err := s.repo.Touch(ctx, id, when); err != nil {
			return Credential{}, err
		}
	}
	_ = current
	return s.repo.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	usage, err := s.repo.CountUsage(ctx, id)
	if err != nil {
		return err
	}
	if usage > 0 {
		return fmt.Errorf("%w: credential is assigned to %d project source(s)", ErrInUse, usage)
	}
	_, scope, name, err := s.repo.SecretRef(ctx, id)
	if err != nil {
		return err
	}
	if s.secrets != nil {
		_ = s.secrets.Delete(ctx, scope, name)
	}
	return s.repo.Delete(ctx, id)
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable")
	}
	return hex.EncodeToString(b)
}

type Module struct {
	service *Service
	audit   *audit.Service
}

func NewModule(service *Service, auditService *audit.Service) *Module {
	return &Module{service: service, audit: auditService}
}
func (m *Module) Name() string { return "credentials" }

func (m *Module) RegisterRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	secure := func(role domain.Role, h http.HandlerFunc) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(role, h))
	}
	mux.Handle("GET /api/v1/credentials", secure(domain.RoleOperator, m.list))
	mux.Handle("POST /api/v1/credentials", secure(domain.RoleOperator, m.create))
	mux.Handle("PATCH /api/v1/credentials/{id}", secure(domain.RoleOperator, m.update))
	mux.Handle("DELETE /api/v1/credentials/{id}", secure(domain.RoleOperator, m.delete))
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.List(r.Context())
	if err != nil {
		m.fail(w, err)
		return
	}
	writeData(w, http.StatusOK, items)
}
func (m *Module) create(w http.ResponseWriter, r *http.Request) {
	var input createInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	actor := actorID(r)
	item, err := m.service.Create(r.Context(), input, actor)
	if err != nil {
		m.fail(w, err)
		return
	}
	_ = m.audit.Record(r.Context(), actor, "credential.create", "credential", &item.ID, map[string]any{"kind": item.Kind}, nil)
	writeData(w, http.StatusCreated, item)
}
func (m *Module) update(w http.ResponseWriter, r *http.Request) {
	var input updateInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	item, err := m.service.Update(r.Context(), r.PathValue("id"), input)
	if err != nil {
		m.fail(w, err)
		return
	}
	actor := actorID(r)
	_ = m.audit.Record(r.Context(), actor, "credential.update", "credential", &item.ID, nil, nil)
	writeData(w, http.StatusOK, item)
}
func (m *Module) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := m.service.Delete(r.Context(), id); err != nil {
		m.fail(w, err)
		return
	}
	actor := actorID(r)
	_ = m.audit.Record(r.Context(), actor, "credential.delete", "credential", &id, nil, nil)
	writeData(w, http.StatusOK, map[string]string{"status": "deleted"})
}
func (m *Module) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "credential not found")
	case errors.Is(err, ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, ErrInUse):
		writeError(w, http.StatusConflict, "credential_in_use", err.Error())
	case strings.Contains(strings.ToLower(err.Error()), "unique constraint"):
		writeError(w, http.StatusConflict, "conflict", "credential name already exists")
	case strings.Contains(err.Error(), "provider unavailable"):
		writeError(w, http.StatusServiceUnavailable, "provider_unavailable", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
	}
}

func actorID(r *http.Request) *string {
	user, ok := api.CurrentUser(r.Context())
	if !ok {
		return nil
	}
	id := user.ID
	return &id
}
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid JSON request")
	}
	return nil
}
func writeData(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}
