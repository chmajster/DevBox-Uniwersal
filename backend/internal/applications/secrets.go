package applications

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

func (s *Service) WithSecretStore(store secrets.SecretStore) *Service {
	s.secretStore = store
	return s
}

func (s *Service) SecretNames(ctx context.Context, id string) ([]string, error) {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.repo.db.QueryContext(ctx, `SELECT name FROM application_secret_bindings WHERE application_id=? AND workload_id IS NULL ORDER BY name`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func (s *Service) PutSecret(ctx context.Context, id, name, value string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if s.secretStore == nil {
		return ErrProviderUnavailable
	}
	if err := s.repo.CheckIdle(ctx, id); err != nil {
		return err
	}
	if !environmentName.MatchString(name) || strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%w: invalid secret name/value", ErrInvalidInput)
	}
	scope := "application/" + id
	previous, previousErr := s.secretStore.Get(ctx, scope, name)
	if previousErr != nil && !errors.Is(previousErr, secrets.ErrNotFound) {
		return previousErr
	}
	if err := s.secretStore.Put(ctx, scope, name, []byte(value)); err != nil {
		return err
	}
	now := dbTime(time.Now())
	_, err := s.repo.db.ExecContext(ctx, `INSERT INTO application_secret_bindings(id,application_id,workload_id,name,secret_scope,secret_name,created_at,updated_at) VALUES(?,?,NULL,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET updated_at=excluded.updated_at`, id+":"+name, id, name, scope, name, now, now)
	if err != nil {
		if previousErr == nil {
			_ = s.secretStore.Put(context.Background(), scope, name, previous)
		} else {
			_ = s.secretStore.Delete(context.Background(), scope, name)
		}
	}
	return err
}

func (s *Service) DeleteSecret(ctx context.Context, id, name string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if s.secretStore == nil {
		return ErrProviderUnavailable
	}
	if err := s.repo.CheckIdle(ctx, id); err != nil {
		return err
	}
	if !environmentName.MatchString(name) {
		return fmt.Errorf("%w: secret name", ErrInvalidInput)
	}
	if err := s.secretStore.Delete(ctx, "application/"+id, name); err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return err
	}
	_, err := s.repo.db.ExecContext(ctx, `DELETE FROM application_secret_bindings WHERE application_id=? AND name=? AND workload_id IS NULL`, id, name)
	return err
}

func (s *Service) runtimeSecrets(ctx context.Context, id string) (map[string]string, error) {
	names, err := s.SecretNames(ctx, id)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	if len(names) > 0 && s.secretStore == nil {
		return nil, ErrProviderUnavailable
	}
	for _, name := range names {
		value, err := s.secretStore.Get(ctx, "application/"+id, name)
		if err != nil {
			return nil, fmt.Errorf("%w: secret %s is unavailable", ErrConfigurationRequired, name)
		}
		result[name] = string(value)
	}
	return result, nil
}

func redactSecretValues(text string, values map[string]string) string {
	for _, value := range values {
		if value != "" {
			text = strings.ReplaceAll(text, value, "***")
		}
	}
	return text
}

func (m *Module) registerSecretRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	secure := func(role domain.Role, handler http.HandlerFunc) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(role, handler))
	}
	mux.Handle("GET /api/v1/applications/{id}/secrets", secure(domain.RoleViewer, m.listSecrets))
	mux.Handle("PUT /api/v1/applications/{id}/secrets/{name}", secure(domain.RoleOperator, m.putSecret))
	mux.Handle("DELETE /api/v1/applications/{id}/secrets/{name}", secure(domain.RoleOperator, m.deleteSecret))
}
func (m *Module) listSecrets(w http.ResponseWriter, r *http.Request) {
	names, err := m.service.SecretNames(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeApplicationData(w, http.StatusOK, names)
}
func (m *Module) putSecret(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Value *string `json:"value"`
	}
	if err := decodeApplicationJSON(w, r, &input, false); err != nil || input.Value == nil {
		writeApplicationError(w, http.StatusBadRequest, "invalid_request", "value is required", nil)
		return
	}
	if err := m.service.PutSecret(r.Context(), r.PathValue("id"), r.PathValue("name"), *input.Value); err != nil {
		m.fail(w, err)
		return
	}
	m.auditSecret(r, "application.secret.set")
	writeApplicationData(w, http.StatusOK, map[string]bool{"saved": true})
}
func (m *Module) deleteSecret(w http.ResponseWriter, r *http.Request) {
	if err := m.service.DeleteSecret(r.Context(), r.PathValue("id"), r.PathValue("name")); err != nil {
		m.fail(w, err)
		return
	}
	m.auditSecret(r, "application.secret.delete")
	writeApplicationData(w, http.StatusOK, map[string]bool{"deleted": true})
}
func (m *Module) auditSecret(r *http.Request, action string) {
	if m.audit != nil {
		id := r.PathValue("id")
		_ = m.audit.Record(r.Context(), applicationActor(r), action, "application", &id, map[string]any{"name": r.PathValue("name")}, remoteAddress(r))
	}
}
