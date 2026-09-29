package gitintegrations

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type TokenReader interface {
	ReadToken(context.Context, string) ([]byte, error)
}
type Integration struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Provider     string `json:"provider"`
	BaseURL      string `json:"base_url"`
	CredentialID string `json:"credential_id"`
	CreatedAt    string `json:"created_at"`
}
type Service struct {
	db          *sql.DB
	credentials TokenReader
	client      *http.Client
}

func NewService(db *sql.DB, credentials TokenReader) *Service {
	return &Service{db: db, credentials: credentials, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// A profile explicitly binds a token to one HTTPS server. Redirects are never
// followed and callers cannot supply an arbitrary URL to the proxy endpoints.
func normalize(i Integration) (Integration, error) {
	i.Name = strings.TrimSpace(i.Name)
	i.Provider = strings.ToLower(strings.TrimSpace(i.Provider))
	i.BaseURL = strings.TrimRight(strings.TrimSpace(i.BaseURL), "/")
	if len(i.Name) < 1 || len(i.Name) > 120 || i.CredentialID == "" {
		return i, fmt.Errorf("name and stored token are required")
	}
	switch i.Provider {
	case "github":
		if i.BaseURL == "" || i.BaseURL == "https://github.com" {
			i.BaseURL = "https://api.github.com"
		}
		if i.BaseURL != "https://api.github.com" {
			return i, fmt.Errorf("GitHub profile must use https://api.github.com")
		}
	case "gitlab":
		if i.BaseURL == "" {
			i.BaseURL = "https://gitlab.com"
		}
		u, err := url.Parse(i.BaseURL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(i.BaseURL, "\r\n\\") {
			return i, fmt.Errorf("GitLab base URL must be a trusted HTTPS server without credentials, query or fragment")
		}
		for _, part := range strings.Split(u.Path, "/") {
			if part == ".." || part == "." {
				return i, fmt.Errorf("invalid GitLab base path")
			}
		}
		if strings.HasSuffix(i.BaseURL, "/api/v4") {
			i.BaseURL = strings.TrimSuffix(i.BaseURL, "/api/v4")
		}
	default:
		return i, fmt.Errorf("provider must be github or gitlab")
	}
	return i, nil
}
func (s *Service) Create(ctx context.Context, i Integration) (Integration, error) {
	i, err := normalize(i)
	if err != nil {
		return i, err
	}
	token, err := s.credentials.ReadToken(ctx, i.CredentialID)
	if err != nil {
		return i, err
	}
	clear(token)
	b := make([]byte, 16)
	if _, err = rand.Read(b); err != nil {
		return i, err
	}
	i.ID = hex.EncodeToString(b)
	i.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `INSERT INTO git_integrations(id,name,provider,base_url,credential_id,created_at) VALUES(?,?,?,?,?,?)`, i.ID, i.Name, i.Provider, i.BaseURL, i.CredentialID, i.CreatedAt)
	if err != nil {
		return Integration{}, fmt.Errorf("cannot save Git integration: name must be unique and credential must exist")
	}
	return i, nil
}
func (s *Service) List(ctx context.Context) ([]Integration, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,provider,base_url,credential_id,created_at FROM git_integrations ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Integration{}
	for rows.Next() {
		var i Integration
		if err = rows.Scan(&i.ID, &i.Name, &i.Provider, &i.BaseURL, &i.CredentialID, &i.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}
func (s *Service) Get(ctx context.Context, id string) (Integration, error) {
	var i Integration
	err := s.db.QueryRowContext(ctx, `SELECT id,name,provider,base_url,credential_id,created_at FROM git_integrations WHERE id=?`, id).Scan(&i.ID, &i.Name, &i.Provider, &i.BaseURL, &i.CredentialID, &i.CreatedAt)
	if err != nil {
		return i, fmt.Errorf("Git integration not found")
	}
	return normalize(i)
}
func (s *Service) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM git_integrations WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("Git integration not found")
	}
	return nil
}
