package sourcecontrol

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/credentials"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

const JobIntegrationSync = "integration.sync"

var (
	ErrIntegrationNotFound = errors.New("source-control integration not found")
	ErrInvalidIntegration  = errors.New("invalid source-control integration")
	ErrIntegrationInUse    = errors.New("source-control integration is in use")
)

type Integration struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Provider        string    `json:"provider"`
	WebURL          string    `json:"web_url"`
	APIURL          string    `json:"api_url"`
	CredentialID    string    `json:"credential_id"`
	Enabled         bool      `json:"enabled"`
	Status          string    `json:"status"`
	AccountUsername string    `json:"account_username,omitempty"`
	AccountID       string    `json:"account_id,omitempty"`
	Scopes          []string  `json:"scopes,omitempty"`
	RepositoryCount int       `json:"repository_count"`
	NamespaceCount  int       `json:"namespace_count"`
	LastTestedAt    *time.Time `json:"last_tested_at,omitempty"`
	LastSyncedAt    *time.Time `json:"last_synced_at,omitempty"`
	CreatedBy       *string   `json:"created_by,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type IntegrationInput struct {
	Name         string `json:"name"`
	Provider     string `json:"provider"`
	WebURL       string `json:"web_url"`
	APIURL       string `json:"api_url"`
	CredentialID string `json:"credential_id"`
	Enabled      *bool  `json:"enabled,omitempty"`
}

type TestResult struct {
	Connected bool      `json:"connected"`
	Status    string    `json:"status"`
	Account   string    `json:"account,omitempty"`
	AccountID string    `json:"account_id,omitempty"`
	Scopes    []string  `json:"scopes,omitempty"`
	TestedAt  time.Time `json:"tested_at"`
	Error     string    `json:"error,omitempty"`
}

type ProjectSourceLink struct {
	ProjectID            string `json:"project_id"`
	IntegrationID        string `json:"integration_id"`
	Provider             string `json:"provider"`
	RepositoryExternalID string `json:"repository_external_id"`
	RepositoryOwner      string `json:"repository_owner"`
	RepositoryPath       string `json:"repository_path"`
	RepositoryName       string `json:"repository_name"`
	CloneURL             string `json:"clone_url"`
	WebURL               string `json:"web_url"`
	DefaultBranch        string `json:"default_branch"`
	CurrentCommit        string `json:"current_commit,omitempty"`
	Branch               string `json:"branch,omitempty"`
}

type PageResult[T any] struct {
	Items []T                         `json:"items"`
	Page  providers.SourceControlPage `json:"page"`
}

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) List(ctx context.Context) ([]Integration, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT id,name,provider,web_url,api_url,credential_id,enabled,status,account_username,account_id,scopes_json,metadata_json,last_tested_at,last_synced_at,created_by,created_at,updated_at FROM source_control_integrations ORDER BY name COLLATE NOCASE")
	if err != nil {
		return nil, fmt.Errorf("list source-control integrations: %w", err)
	}
	defer rows.Close()
	items := []Integration{}
	for rows.Next() {
		item, err := scanIntegration(rows.Scan)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id string) (Integration, error) {
	item, err := scanIntegration(r.db.QueryRowContext(ctx, "SELECT id,name,provider,web_url,api_url,credential_id,enabled,status,account_username,account_id,scopes_json,metadata_json,last_tested_at,last_synced_at,created_by,created_at,updated_at FROM source_control_integrations WHERE id=?", id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Integration{}, ErrIntegrationNotFound
	}
	return item, err
}

func (r *Repository) Create(ctx context.Context, item Integration) error {
	scopes, _ := json.Marshal(item.Scopes)
	metadata, _ := json.Marshal(map[string]int{"repository_count": item.RepositoryCount, "namespace_count": item.NamespaceCount})
	_, err := r.db.ExecContext(ctx, "INSERT INTO source_control_integrations(id,name,provider,web_url,api_url,credential_id,enabled,status,account_username,account_id,scopes_json,metadata_json,last_tested_at,last_synced_at,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		item.ID, item.Name, item.Provider, item.WebURL, item.APIURL, item.CredentialID, boolInt(item.Enabled), item.Status, nullable(item.AccountUsername), nullable(item.AccountID), string(scopes), string(metadata), timePtrString(item.LastTestedAt), timePtrString(item.LastSyncedAt), item.CreatedBy, formatTime(item.CreatedAt), formatTime(item.UpdatedAt))
	return err
}

func (r *Repository) Update(ctx context.Context, item Integration) error {
	scopes, _ := json.Marshal(item.Scopes)
	metadata, _ := json.Marshal(map[string]int{"repository_count": item.RepositoryCount, "namespace_count": item.NamespaceCount})
	res, err := r.db.ExecContext(ctx, "UPDATE source_control_integrations SET name=?,provider=?,web_url=?,api_url=?,credential_id=?,enabled=?,status=?,account_username=?,account_id=?,scopes_json=?,metadata_json=?,last_tested_at=?,last_synced_at=?,updated_at=? WHERE id=?",
		item.Name, item.Provider, item.WebURL, item.APIURL, item.CredentialID, boolInt(item.Enabled), item.Status, nullable(item.AccountUsername), nullable(item.AccountID), string(scopes), string(metadata), timePtrString(item.LastTestedAt), timePtrString(item.LastSyncedAt), formatTime(item.UpdatedAt), item.ID)
	if err != nil {
		return err
	}
	if count, _ := res.RowsAffected(); count == 0 {
		return ErrIntegrationNotFound
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	var count int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM project_source_control WHERE integration_id=?", id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%w: integration is connected to %d project(s)", ErrIntegrationInUse, count)
	}
	res, err := r.db.ExecContext(ctx, "DELETE FROM source_control_integrations WHERE id=?", id)
	if err != nil {
		return err
	}
	if count, _ := res.RowsAffected(); count == 0 {
		return ErrIntegrationNotFound
	}
	return nil
}

func (r *Repository) LinkProject(ctx context.Context, link ProjectSourceLink) error {
	now := formatTime(time.Now().UTC())
	_, err := r.db.ExecContext(ctx, "INSERT INTO project_source_control(project_id,integration_id,provider,repository_external_id,repository_owner,repository_path,repository_name,clone_url,web_url,default_branch,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(project_id) DO UPDATE SET integration_id=excluded.integration_id,provider=excluded.provider,repository_external_id=excluded.repository_external_id,repository_owner=excluded.repository_owner,repository_path=excluded.repository_path,repository_name=excluded.repository_name,clone_url=excluded.clone_url,web_url=excluded.web_url,default_branch=excluded.default_branch,updated_at=excluded.updated_at",
		link.ProjectID, link.IntegrationID, link.Provider, link.RepositoryExternalID, link.RepositoryOwner, link.RepositoryPath, link.RepositoryName, link.CloneURL, link.WebURL, link.DefaultBranch, now, now)
	return err
}

func (r *Repository) ProjectLink(ctx context.Context, projectID string) (ProjectSourceLink, error) {
	var item ProjectSourceLink
	var commit, branch sql.NullString
	err := r.db.QueryRowContext(ctx, "SELECT s.project_id,s.integration_id,s.provider,s.repository_external_id,s.repository_owner,s.repository_path,s.repository_name,s.clone_url,s.web_url,s.default_branch,p.current_commit,ps.reference FROM project_source_control s JOIN projects p ON p.id=s.project_id LEFT JOIN project_sources ps ON ps.project_id=s.project_id WHERE s.project_id=?", projectID).
		Scan(&item.ProjectID, &item.IntegrationID, &item.Provider, &item.RepositoryExternalID, &item.RepositoryOwner, &item.RepositoryPath, &item.RepositoryName, &item.CloneURL, &item.WebURL, &item.DefaultBranch, &commit, &branch)
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectSourceLink{}, ErrIntegrationNotFound
	}
	if commit.Valid {
		item.CurrentCommit = commit.String
	}
	if branch.Valid {
		item.Branch = branch.String
	}
	return item, err
}

func scanIntegration(scan func(dest ...any) error) (Integration, error) {
	var item Integration
	var enabled int
	var account, accountID, tested, synced, createdBy sql.NullString
	var scopesJSON, metadataJSON, created, updated string
	if err := scan(&item.ID, &item.Name, &item.Provider, &item.WebURL, &item.APIURL, &item.CredentialID, &enabled, &item.Status, &account, &accountID, &scopesJSON, &metadataJSON, &tested, &synced, &createdBy, &created, &updated); err != nil {
		return Integration{}, err
	}
	item.Enabled = enabled != 0
	if account.Valid {
		item.AccountUsername = account.String
	}
	if accountID.Valid {
		item.AccountID = accountID.String
	}
	if createdBy.Valid {
		value := createdBy.String
		item.CreatedBy = &value
	}
	_ = json.Unmarshal([]byte(scopesJSON), &item.Scopes)
	var metadata struct {
		RepositoryCount int `json:"repository_count"`
		NamespaceCount  int `json:"namespace_count"`
	}
	_ = json.Unmarshal([]byte(metadataJSON), &metadata)
	item.RepositoryCount = metadata.RepositoryCount
	item.NamespaceCount = metadata.NamespaceCount
	var err error
	item.CreatedAt, err = parseTime(created)
	if err != nil {
		return Integration{}, err
	}
	item.UpdatedAt, err = parseTime(updated)
	if err != nil {
		return Integration{}, err
	}
	if tested.Valid {
		value, parseErr := parseTime(tested.String)
		if parseErr != nil {
			return Integration{}, parseErr
		}
		item.LastTestedAt = &value
	}
	if synced.Valid {
		value, parseErr := parseTime(synced.String)
		if parseErr != nil {
			return Integration{}, parseErr
		}
		item.LastSyncedAt = &value
	}
	return item, nil
}

type Service struct {
	repo        *Repository
	credentials *credentials.Repository
	secrets     secrets.SecretStore
	client      *http.Client
	jobs        jobs.JobRunner
}

func NewService(repo *Repository, credentialRepo *credentials.Repository, secretStore secrets.SecretStore, client *http.Client, runner jobs.JobRunner) *Service {
	return &Service{repo: repo, credentials: credentialRepo, secrets: secretStore, client: client, jobs: runner}
}

func (s *Service) List(ctx context.Context) ([]Integration, error) { return s.repo.List(ctx) }
func (s *Service) Get(ctx context.Context, id string) (Integration, error) { return s.repo.Get(ctx, id) }

func (s *Service) Create(ctx context.Context, input IntegrationInput, actor *string) (Integration, error) {
	item, err := s.prepareInput(ctx, Integration{}, input, true)
	if err != nil {
		return Integration{}, err
	}
	item.ID = integrationID()
	item.CreatedBy = actor
	now := time.Now().UTC()
	item.CreatedAt, item.UpdatedAt = now, now
	if err := s.repo.Create(ctx, item); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Integration{}, fmt.Errorf("%w: integration name already exists", ErrInvalidIntegration)
		}
		return Integration{}, err
	}
	return s.repo.Get(ctx, item.ID)
}

func (s *Service) Update(ctx context.Context, id string, input IntegrationInput) (Integration, error) {
	current, err := s.repo.Get(ctx, id)
	if err != nil {
		return Integration{}, err
	}
	item, err := s.prepareInput(ctx, current, input, false)
	if err != nil {
		return Integration{}, err
	}
	item.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, item); err != nil {
		return Integration{}, err
	}
	return s.repo.Get(ctx, id)
}

func (s *Service) prepareInput(ctx context.Context, current Integration, input IntegrationInput, creating bool) (Integration, error) {
	item := current
	if creating || strings.TrimSpace(input.Name) != "" {
		item.Name = strings.TrimSpace(input.Name)
	}
	if item.Name == "" || len(item.Name) > 120 {
		return Integration{}, fmt.Errorf("%w: name is required and must be at most 120 characters", ErrInvalidIntegration)
	}
	if creating || strings.TrimSpace(input.Provider) != "" {
		item.Provider = strings.ToLower(strings.TrimSpace(input.Provider))
	}
	if item.Provider != "github" && item.Provider != "gitlab" {
		return Integration{}, fmt.Errorf("%w: provider must be github or gitlab", ErrInvalidIntegration)
	}
	if creating || strings.TrimSpace(input.CredentialID) != "" {
		item.CredentialID = strings.TrimSpace(input.CredentialID)
	}
	if item.CredentialID == "" {
		return Integration{}, fmt.Errorf("%w: central credential is required", ErrInvalidIntegration)
	}
	if input.Enabled != nil {
		item.Enabled = *input.Enabled
	} else if creating {
		item.Enabled = true
	}
	webURL := strings.TrimSpace(input.WebURL)
	apiURL := strings.TrimSpace(input.APIURL)
	if !creating {
		if webURL == "" {
			webURL = item.WebURL
		}
		if apiURL == "" {
			apiURL = item.APIURL
		}
	}
	if webURL == "" {
		webURL, _ = DefaultURLs(item.Provider)
	}
	if apiURL == "" {
		apiURL = deriveAPIURL(item.Provider, webURL)
	}
	item.WebURL, item.APIURL = webURL, apiURL

	provider, err := s.providerForValues(ctx, item.Provider, item.WebURL, item.APIURL, item.CredentialID)
	if err != nil {
		return Integration{}, err
	}
	user, err := provider.TestConnection(ctx)
	now := time.Now().UTC()
	item.LastTestedAt = &now
	if err != nil {
		item.Status = integrationStatusForError(err)
		return Integration{}, fmt.Errorf("%w: connection test failed: %v", ErrInvalidIntegration, err)
	}
	item.AccountUsername = user.Username
	item.AccountID = user.ID
	item.Scopes = append([]string(nil), user.Scopes...)
	if item.Enabled {
		item.Status = "connected"
	} else {
		item.Status = "disabled"
	}
	return item, nil
}

func (s *Service) Delete(ctx context.Context, id string) error { return s.repo.Delete(ctx, id) }

func (s *Service) Test(ctx context.Context, id string) (TestResult, error) {
	item, err := s.repo.Get(ctx, id)
	if err != nil {
		return TestResult{}, err
	}
	provider, err := s.provider(ctx, item)
	if err != nil {
		return TestResult{}, err
	}
	now := time.Now().UTC()
	user, testErr := provider.TestConnection(ctx)
	item.LastTestedAt = &now
	if testErr != nil {
		item.Status = integrationStatusForError(testErr)
		item.UpdatedAt = now
		_ = s.repo.Update(context.Background(), item)
		return TestResult{Connected: false, Status: item.Status, TestedAt: now, Error: testErr.Error()}, nil
	}
	item.AccountUsername, item.AccountID, item.Scopes = user.Username, user.ID, append([]string(nil), user.Scopes...)
	if item.Enabled {
		item.Status = "connected"
	} else {
		item.Status = "disabled"
	}
	item.UpdatedAt = now
	if err := s.repo.Update(ctx, item); err != nil {
		return TestResult{}, err
	}
	return TestResult{Connected: item.Enabled, Status: item.Status, Account: user.Username, AccountID: user.ID, Scopes: user.Scopes, TestedAt: now}, nil
}

func (s *Service) EnqueueSync(ctx context.Context, id string, actor *string) (domain.Job, error) {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return domain.Job{}, err
	}
	return s.jobs.Enqueue(ctx, jobs.Request{Type: JobIntegrationSync, RequestedBy: actor, Payload: map[string]any{"integration_id": id}})
}

func (s *Service) Namespaces(ctx context.Context, id, search string, page, perPage int) (PageResult[providers.SourceControlNamespace], error) {
	item, err := s.repo.Get(ctx, id)
	if err != nil {
		return PageResult[providers.SourceControlNamespace]{}, err
	}
	provider, err := s.provider(ctx, item)
	if err != nil {
		return PageResult[providers.SourceControlNamespace]{}, err
	}
	items, pagination, err := provider.ListNamespaces(ctx, search, page, perPage)
	return PageResult[providers.SourceControlNamespace]{Items: items, Page: pagination}, err
}

func (s *Service) Repositories(ctx context.Context, id, namespace, search string, page, perPage int) (PageResult[providers.SourceControlRepository], error) {
	item, err := s.repo.Get(ctx, id)
	if err != nil {
		return PageResult[providers.SourceControlRepository]{}, err
	}
	provider, err := s.provider(ctx, item)
	if err != nil {
		return PageResult[providers.SourceControlRepository]{}, err
	}
	items, pagination, err := provider.ListRepositories(ctx, namespace, search, page, perPage)
	return PageResult[providers.SourceControlRepository]{Items: items, Page: pagination}, err
}

func (s *Service) Branches(ctx context.Context, id, repository, search string, page, perPage int) (PageResult[providers.SourceControlBranch], error) {
	item, err := s.repo.Get(ctx, id)
	if err != nil {
		return PageResult[providers.SourceControlBranch]{}, err
	}
	provider, err := s.provider(ctx, item)
	if err != nil {
		return PageResult[providers.SourceControlBranch]{}, err
	}
	items, pagination, err := provider.ListBranches(ctx, repository, search, page, perPage)
	return PageResult[providers.SourceControlBranch]{Items: items, Page: pagination}, err
}

func (s *Service) ProjectSource(ctx context.Context, projectID string) (ProjectSourceLink, error) {
	return s.repo.ProjectLink(ctx, projectID)
}

func (s *Service) PullRequests(ctx context.Context, projectID, state string, page, perPage int) (PageResult[providers.SourceControlPullRequest], error) {
	link, err := s.repo.ProjectLink(ctx, projectID)
	if err != nil {
		return PageResult[providers.SourceControlPullRequest]{}, err
	}
	item, err := s.repo.Get(ctx, link.IntegrationID)
	if err != nil {
		return PageResult[providers.SourceControlPullRequest]{}, err
	}
	provider, err := s.provider(ctx, item)
	if err != nil {
		return PageResult[providers.SourceControlPullRequest]{}, err
	}
	items, pagination, err := provider.ListPullRequests(ctx, link.RepositoryPath, state, page, perPage)
	return PageResult[providers.SourceControlPullRequest]{Items: items, Page: pagination}, err
}

func (s *Service) CI(ctx context.Context, projectID string) (providers.SourceControlCIStatus, error) {
	link, err := s.repo.ProjectLink(ctx, projectID)
	if err != nil {
		return providers.SourceControlCIStatus{}, err
	}
	if link.CurrentCommit == "" {
		return providers.SourceControlCIStatus{Available: false, Provider: link.Provider, Message: "Project does not have a current commit yet"}, nil
	}
	item, err := s.repo.Get(ctx, link.IntegrationID)
	if err != nil {
		return providers.SourceControlCIStatus{}, err
	}
	provider, err := s.provider(ctx, item)
	if err != nil {
		return providers.SourceControlCIStatus{}, err
	}
	return provider.GetCommitStatus(ctx, link.RepositoryPath, link.CurrentCommit)
}

func (s *Service) ProjectWebLink(ctx context.Context, projectID, kind, identifier string) (string, error) {
	link, err := s.repo.ProjectLink(ctx, projectID)
	if err != nil {
		return "", err
	}
	item, err := s.repo.Get(ctx, link.IntegrationID)
	if err != nil {
		return "", err
	}
	provider, err := s.provider(ctx, item)
	if err != nil {
		return "", err
	}
	return provider.BuildWebURL(link.RepositoryPath, kind, identifier)
}

func (s *Service) ResolveProjectSource(ctx context.Context, integrationID, repositoryPath, branch string) (providers.ProjectSourceResolution, error) {
	item, err := s.repo.Get(ctx, integrationID)
	if err != nil {
		return providers.ProjectSourceResolution{}, err
	}
	if !item.Enabled || item.Status == "authentication_failed" || item.Status == "unavailable" {
		return providers.ProjectSourceResolution{}, fmt.Errorf("%w: integration is not usable", ErrInvalidIntegration)
	}
	provider, err := s.provider(ctx, item)
	if err != nil {
		return providers.ProjectSourceResolution{}, err
	}
	repository, err := provider.GetRepository(ctx, repositoryPath)
	if err != nil {
		return providers.ProjectSourceResolution{}, err
	}
	if branch == "" {
		branch = repository.DefaultBranch
	}
	if branch == "" {
		branch = "main"
	}
	return providers.ProjectSourceResolution{
		IntegrationID: integrationID, Provider: item.Provider, CredentialID: item.CredentialID,
		Repository: repository, Branch: branch,
	}, nil
}

func (s *Service) LinkProjectSource(ctx context.Context, projectID string, resolution providers.ProjectSourceResolution) error {
	repository := resolution.Repository
	return s.repo.LinkProject(ctx, ProjectSourceLink{
		ProjectID: projectID, IntegrationID: resolution.IntegrationID, Provider: resolution.Provider,
		RepositoryExternalID: repository.ID, RepositoryOwner: repository.Owner, RepositoryPath: repository.Path,
		RepositoryName: repository.Name, CloneURL: repository.CloneURL, WebURL: repository.WebURL, DefaultBranch: repository.DefaultBranch,
	})
}

func (s *Service) provider(ctx context.Context, item Integration) (providers.SourceControlIntegrationProvider, error) {
	if !item.Enabled {
		return nil, fmt.Errorf("%w: integration is disabled", ErrInvalidIntegration)
	}
	return s.providerForValues(ctx, item.Provider, item.WebURL, item.APIURL, item.CredentialID)
}

func (s *Service) providerForValues(ctx context.Context, providerName, webURL, apiURL, credentialID string) (providers.SourceControlIntegrationProvider, error) {
	if s.secrets == nil {
		return nil, errors.New("secret store is not configured")
	}
	kind, scope, name, err := s.credentials.SecretRef(ctx, credentialID)
	if err != nil {
		return nil, fmt.Errorf("%w: central credential was not found", ErrInvalidIntegration)
	}
	if kind != "token" {
		return nil, fmt.Errorf("%w: GitHub/GitLab API integration requires a token credential", ErrInvalidIntegration)
	}
	value, err := s.secrets.Get(ctx, scope, name)
	if err != nil {
		return nil, errors.New("source-control credential secret is unavailable")
	}
	return NewProvider(providerName, webURL, apiURL, string(value), s.client)
}

func integrationStatusForError(err error) string {
	switch {
	case errors.Is(err, ErrAuthentication), errors.Is(err, ErrPermission):
		return "authentication_failed"
	case errors.As(err, new(*RateLimitError)):
		return "degraded"
	default:
		return "unavailable"
	}
}

func deriveAPIURL(provider, webURL string) string {
	webURL = strings.TrimRight(strings.TrimSpace(webURL), "/")
	if provider == "github" {
		if strings.EqualFold(webURL, "https://github.com") {
			return "https://api.github.com"
		}
		return webURL + "/api/v3"
	}
	return webURL + "/api/v4"
}

type SyncJobHandler struct {
	service *Service
	logger  interface {
		Log(ctx context.Context, jobID, level, message string, fields map[string]any) error
	}
	audit *audit.Service
}

func NewSyncJobHandler(service *Service, logger interface {
	Log(ctx context.Context, jobID, level, message string, fields map[string]any) error
}, auditService *audit.Service) *SyncJobHandler {
	return &SyncJobHandler{service: service, logger: logger, audit: auditService}
}

func (h *SyncJobHandler) Type() string { return JobIntegrationSync }

func (h *SyncJobHandler) Run(ctx context.Context, job domain.Job) (map[string]any, error) {
	integrationID, _ := job.Payload["integration_id"].(string)
	if integrationID == "" {
		return nil, errors.New("integration sync job is missing integration_id")
	}
	item, err := h.service.repo.Get(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	provider, err := h.service.provider(ctx, item)
	if err != nil {
		return nil, err
	}
	_ = h.logger.Log(ctx, job.ID, "info", "integration.sync.account", map[string]any{"integration_id": item.ID})
	user, err := provider.CurrentUser(ctx)
	if err != nil {
		item.Status = integrationStatusForError(err)
		item.UpdatedAt = time.Now().UTC()
		_ = h.service.repo.Update(context.Background(), item)
		return nil, err
	}
	namespaceCount, err := countNamespaces(ctx, provider)
	if err != nil {
		return nil, err
	}
	repositoryCount, err := countRepositories(ctx, provider)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	item.AccountUsername, item.AccountID, item.Scopes = user.Username, user.ID, append([]string(nil), user.Scopes...)
	item.NamespaceCount, item.RepositoryCount = namespaceCount, repositoryCount
	item.Status = "connected"
	item.LastSyncedAt = &now
	item.LastTestedAt = &now
	item.UpdatedAt = now
	if err := h.service.repo.Update(ctx, item); err != nil {
		return nil, err
	}
	if h.audit != nil {
		id := item.ID
		_ = h.audit.Record(context.Background(), job.RequestedBy, "integration.synced", "source_control_integration", &id, map[string]any{"repositories": repositoryCount, "namespaces": namespaceCount}, nil)
	}
	_ = h.logger.Log(ctx, job.ID, "info", "integration.sync.completed", map[string]any{"repositories": repositoryCount, "namespaces": namespaceCount})
	return map[string]any{"integration_id": item.ID, "repositories": repositoryCount, "namespaces": namespaceCount}, nil
}

func countNamespaces(ctx context.Context, provider providers.SourceControlIntegrationProvider) (int, error) {
	total := 0
	page := 1
	for {
		items, pagination, err := provider.ListNamespaces(ctx, "", page, 100)
		if err != nil {
			return 0, err
		}
		total += len(items)
		if pagination.NextPage == 0 {
			break
		}
		page = pagination.NextPage
		if page > 10000 {
			return 0, errors.New("namespace pagination exceeded safety limit")
		}
	}
	return total, nil
}

func countRepositories(ctx context.Context, provider providers.SourceControlIntegrationProvider) (int, error) {
	total := 0
	page := 1
	for {
		items, pagination, err := provider.ListRepositories(ctx, "", "", page, 100)
		if err != nil {
			return 0, err
		}
		total += len(items)
		if pagination.NextPage == 0 {
			break
		}
		page = pagination.NextPage
		if page > 10000 {
			return 0, errors.New("repository pagination exceeded safety limit")
		}
	}
	return total, nil
}

func integrationID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		panic("crypto/rand unavailable")
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", buffer[0:4], buffer[4:6], buffer[6:8], buffer[8:10], buffer[10:16])
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullable(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func timePtrString(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func parseTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid source-control timestamp %q", value)
}

func parsePositiveInt(value string, fallback int) int {
	result, err := strconv.Atoi(value)
	if err != nil || result < 1 {
		return fallback
	}
	return result
}

func sanitizeSearch(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 200 {
		value = value[:200]
	}
	return value
}

func repositoryNameFromURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	name := strings.TrimSuffix(strings.Trim(filepathBase(parsed.Path), "/"), ".git")
	return strings.TrimSpace(name)
}

func filepathBase(value string) string {
	value = strings.TrimRight(value, "/")
	if index := strings.LastIndex(value, "/"); index >= 0 {
		return value[index+1:]
	}
	return value
}
