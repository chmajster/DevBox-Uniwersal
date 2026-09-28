package sourcecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

var (
	ErrAuthentication = errors.New("source-control authentication failed")
	ErrPermission     = errors.New("source-control permission denied")
	ErrNotFound       = errors.New("source-control resource not found")
)

type RateLimitError struct {
	Remaining int
	ResetAt   time.Time
}

func (e *RateLimitError) Error() string {
	if e.ResetAt.IsZero() {
		return "source-control API rate limit exceeded"
	}
	return fmt.Sprintf("source-control API rate limit exceeded; reset at %s", e.ResetAt.UTC().Format(time.RFC3339))
}

type providerClient struct {
	provider string
	webURL   *url.URL
	apiURL   *url.URL
	token    string
	client   *http.Client
}

func NewProvider(provider, webURL, apiURL, token string, client *http.Client) (providers.SourceControlIntegrationProvider, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "github" && provider != "gitlab" {
		return nil, fmt.Errorf("unsupported source-control provider %q", provider)
	}
	web, err := validateBaseURL(webURL)
	if err != nil {
		return nil, fmt.Errorf("web URL: %w", err)
	}
	api, err := validateBaseURL(apiURL)
	if err != nil {
		return nil, fmt.Errorf("API URL: %w", err)
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("source-control token is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	base := providerClient{provider: provider, webURL: web, apiURL: api, token: token, client: client}
	if provider == "github" {
		return &githubProvider{providerClient: base}, nil
	}
	return &gitlabProvider{providerClient: base}, nil
}

func DefaultURLs(provider string) (webURL, apiURL string) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "github":
		return "https://github.com", "https://api.github.com"
	case "gitlab":
		return "https://gitlab.com", "https://gitlab.com/api/v4"
	default:
		return "", ""
	}
}

func validateBaseURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" {
		return nil, errors.New("absolute URL is required")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("URL must not contain credentials, query or fragment")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopbackHost(parsed.Hostname())) {
		return nil, errors.New("HTTPS is required")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c *providerClient) endpoint(path string, query url.Values) string {
	base := *c.apiURL
	base.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(path, "/")
	base.RawQuery = query.Encode()
	return base.String()
}

func (c *providerClient) request(ctx context.Context, method, path string, query url.Values, target any) (http.Header, error) {
	if method == "" {
		method = http.MethodGet
	}
	attempts := 1
	if method == http.MethodGet {
		attempts = 3
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(attempt) * 200 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		request, err := http.NewRequestWithContext(ctx, method, c.endpoint(path, query), nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("User-Agent", "DevBox-Universal")
		if c.provider == "github" {
			request.Header.Set("Authorization", "Bearer "+c.token)
			request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		} else {
			request.Header.Set("PRIVATE-TOKEN", c.token)
		}
		response, err := c.client.Do(request)
		if err != nil {
			lastErr = err
			if isRetryableNetworkError(err) && attempt+1 < attempts {
				continue
			}
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 32<<20))
		_ = response.Body.Close()
		if readErr != nil {
			return response.Header, readErr
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			if target != nil && len(body) > 0 {
				if err := json.Unmarshal(body, target); err != nil {
					return response.Header, fmt.Errorf("decode %s API response: %w", c.provider, err)
				}
			}
			return response.Header, nil
		}
		err = c.apiError(response, body)
		lastErr = err
		if retryableStatus(response.StatusCode) && attempt+1 < attempts {
			continue
		}
		return response.Header, err
	}
	return nil, lastErr
}

func (c *providerClient) apiError(response *http.Response, body []byte) error {
	if response.StatusCode == http.StatusUnauthorized {
		return ErrAuthentication
	}
	if response.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if response.StatusCode == http.StatusTooManyRequests || rateLimited(response.Header) {
		return &RateLimitError{Remaining: parseIntHeader(response.Header.Get("X-RateLimit-Remaining")), ResetAt: parseReset(response.Header)}
	}
	if response.StatusCode == http.StatusForbidden {
		return ErrPermission
	}
	var payload struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	_ = json.Unmarshal(body, &payload)
	message := strings.TrimSpace(payload.Message)
	if message == "" {
		message = strings.TrimSpace(payload.Error)
	}
	if message == "" {
		message = response.Status
	}
	if len(message) > 500 {
		message = message[:500]
	}
	return fmt.Errorf("%s API: %s", c.provider, message)
}

func isRetryableNetworkError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary())
}

func retryableStatus(status int) bool {
	return status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func rateLimited(headers http.Header) bool {
	return headers.Get("X-RateLimit-Remaining") == "0" && headers.Get("X-RateLimit-Limit") != ""
}

func parseReset(headers http.Header) time.Time {
	value := strings.TrimSpace(headers.Get("X-RateLimit-Reset"))
	if value != "" {
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
			return time.Unix(seconds, 0).UTC()
		}
	}
	if value = strings.TrimSpace(headers.Get("RateLimit-Reset")); value != "" {
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
			return time.Unix(seconds, 0).UTC()
		}
	}
	if retry := strings.TrimSpace(headers.Get("Retry-After")); retry != "" {
		if seconds, err := strconv.Atoi(retry); err == nil {
			return time.Now().UTC().Add(time.Duration(seconds) * time.Second)
		}
	}
	return time.Time{}
}

func parseIntHeader(value string) int {
	result, _ := strconv.Atoi(strings.TrimSpace(value))
	return result
}

func pageValues(page, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 50
	}
	if perPage > 100 {
		perPage = 100
	}
	return page, perPage
}

func githubNextPage(headers http.Header) int {
	for _, part := range strings.Split(headers.Get("Link"), ",") {
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start < 0 || end <= start {
			continue
		}
		parsed, err := url.Parse(strings.TrimSpace(part[start+1 : end]))
		if err == nil {
			value, _ := strconv.Atoi(parsed.Query().Get("page"))
			return value
		}
	}
	return 0
}

type githubProvider struct{ providerClient }

type githubUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	HTMLURL   string `json:"html_url"`
}

func (p *githubProvider) TestConnection(ctx context.Context) (providers.SourceControlUser, error) {
	return p.CurrentUser(ctx)
}

func (p *githubProvider) CurrentUser(ctx context.Context) (providers.SourceControlUser, error) {
	var payload githubUser
	headers, err := p.request(ctx, http.MethodGet, "/user", nil, &payload)
	if err != nil {
		return providers.SourceControlUser{}, err
	}
	scopes := splitHeaderList(headers.Get("X-OAuth-Scopes"))
	return providers.SourceControlUser{ID: strconv.FormatInt(payload.ID, 10), Username: payload.Login, Name: payload.Name, WebURL: payload.HTMLURL, Scopes: scopes}, nil
}

func (p *githubProvider) ListNamespaces(ctx context.Context, search string, page, perPage int) ([]providers.SourceControlNamespace, providers.SourceControlPage, error) {
	page, perPage = pageValues(page, perPage)
	user, err := p.CurrentUser(ctx)
	if err != nil {
		return nil, providers.SourceControlPage{}, err
	}
	items := []providers.SourceControlNamespace{{ID: user.ID, Name: user.Username, Path: user.Username, Kind: "user", WebURL: user.WebURL}}
	var organizations []struct {
		ID      int64  `json:"id"`
		Login   string `json:"login"`
		HTMLURL string `json:"html_url"`
	}
	query := url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)}}
	headers, err := p.request(ctx, http.MethodGet, "/user/orgs", query, &organizations)
	if err != nil {
		return nil, providers.SourceControlPage{}, err
	}
	for _, organization := range organizations {
		if search != "" && !strings.Contains(strings.ToLower(organization.Login), strings.ToLower(search)) {
			continue
		}
		items = append(items, providers.SourceControlNamespace{ID: strconv.FormatInt(organization.ID, 10), Name: organization.Login, Path: organization.Login, Kind: "organization", WebURL: organization.HTMLURL})
	}
	return items, providers.SourceControlPage{Page: page, PerPage: perPage, NextPage: githubNextPage(headers)}, nil
}

type githubRepository struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	CloneURL      string `json:"clone_url"`
	SSHURL        string `json:"ssh_url"`
	HTMLURL       string `json:"html_url"`
	DefaultBranch string `json:"default_branch"`
	Visibility    string `json:"visibility"`
	Private       bool   `json:"private"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

func (p *githubProvider) ListRepositories(ctx context.Context, namespace, search string, page, perPage int) ([]providers.SourceControlRepository, providers.SourceControlPage, error) {
	page, perPage = pageValues(page, perPage)
	query := url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)}, "sort": {"updated"}, "direction": {"desc"}}
	path := "/user/repos"
	if namespace != "" {
		path = "/orgs/" + url.PathEscape(namespace) + "/repos"
		query.Set("type", "all")
	} else {
		query.Set("affiliation", "owner,collaborator,organization_member")
		query.Set("visibility", "all")
	}
	var payload []githubRepository
	headers, err := p.request(ctx, http.MethodGet, path, query, &payload)
	if err != nil {
		if namespace != "" && errors.Is(err, ErrNotFound) {
			path = "/users/" + url.PathEscape(namespace) + "/repos"
			headers, err = p.request(ctx, http.MethodGet, path, query, &payload)
		}
		if err != nil {
			return nil, providers.SourceControlPage{}, err
		}
	}
	items := make([]providers.SourceControlRepository, 0, len(payload))
	for _, repository := range payload {
		if search != "" && !strings.Contains(strings.ToLower(repository.FullName), strings.ToLower(search)) {
			continue
		}
		items = append(items, mapGitHubRepository(repository))
	}
	return items, providers.SourceControlPage{Page: page, PerPage: perPage, NextPage: githubNextPage(headers)}, nil
}

func (p *githubProvider) GetRepository(ctx context.Context, repository string) (providers.SourceControlRepository, error) {
	repository, err := validateRepositoryPath(repository)
	if err != nil {
		return providers.SourceControlRepository{}, err
	}
	var payload githubRepository
	_, err = p.request(ctx, http.MethodGet, "/repos/"+repository, nil, &payload)
	if err != nil {
		return providers.SourceControlRepository{}, err
	}
	return mapGitHubRepository(payload), nil
}

func mapGitHubRepository(repository githubRepository) providers.SourceControlRepository {
	visibility := repository.Visibility
	if visibility == "" {
		if repository.Private {
			visibility = "private"
		} else {
			visibility = "public"
		}
	}
	return providers.SourceControlRepository{
		ID: strconv.FormatInt(repository.ID, 10), Owner: repository.Owner.Login, Path: repository.FullName, Name: repository.Name,
		CloneURL: repository.CloneURL, SSHURL: repository.SSHURL, WebURL: repository.HTMLURL, DefaultBranch: repository.DefaultBranch,
		Visibility: visibility, Namespace: repository.Owner.Login,
	}
}

func (p *githubProvider) ListBranches(ctx context.Context, repository, search string, page, perPage int) ([]providers.SourceControlBranch, providers.SourceControlPage, error) {
	repository, err := validateRepositoryPath(repository)
	if err != nil {
		return nil, providers.SourceControlPage{}, err
	}
	page, perPage = pageValues(page, perPage)
	var payload []struct {
		Name      string `json:"name"`
		Protected bool   `json:"protected"`
		Commit    struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	headers, err := p.request(ctx, http.MethodGet, "/repos/"+repository+"/branches", url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)}}, &payload)
	if err != nil {
		return nil, providers.SourceControlPage{}, err
	}
	repo, _ := p.GetRepository(ctx, repository)
	items := []providers.SourceControlBranch{}
	for _, branch := range payload {
		if search != "" && !strings.Contains(strings.ToLower(branch.Name), strings.ToLower(search)) {
			continue
		}
		items = append(items, providers.SourceControlBranch{Name: branch.Name, CommitSHA: branch.Commit.SHA, Default: branch.Name == repo.DefaultBranch, Protected: branch.Protected})
	}
	return items, providers.SourceControlPage{Page: page, PerPage: perPage, NextPage: githubNextPage(headers)}, nil
}

func (p *githubProvider) ListTags(ctx context.Context, repository string, page, perPage int) ([]providers.SourceControlTag, providers.SourceControlPage, error) {
	repository, err := validateRepositoryPath(repository)
	if err != nil {
		return nil, providers.SourceControlPage{}, err
	}
	page, perPage = pageValues(page, perPage)
	var payload []struct {
		Name   string `json:"name"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	headers, err := p.request(ctx, http.MethodGet, "/repos/"+repository+"/tags", url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)}}, &payload)
	if err != nil {
		return nil, providers.SourceControlPage{}, err
	}
	items := make([]providers.SourceControlTag, 0, len(payload))
	for _, tag := range payload {
		items = append(items, providers.SourceControlTag{Name: tag.Name, CommitSHA: tag.Commit.SHA})
	}
	return items, providers.SourceControlPage{Page: page, PerPage: perPage, NextPage: githubNextPage(headers)}, nil
}

func (p *githubProvider) ListPullRequests(ctx context.Context, repository, state string, page, perPage int) ([]providers.SourceControlPullRequest, providers.SourceControlPage, error) {
	repository, err := validateRepositoryPath(repository)
	if err != nil {
		return nil, providers.SourceControlPage{}, err
	}
	page, perPage = pageValues(page, perPage)
	queryState := "all"
	if state == "open" || state == "closed" {
		queryState = state
	}
	var payload []struct {
		Number   int        `json:"number"`
		Title    string     `json:"title"`
		State    string     `json:"state"`
		MergedAt *time.Time `json:"merged_at"`
		Updated  time.Time  `json:"updated_at"`
		HTMLURL  string     `json:"html_url"`
		User     struct {
			Login string `json:"login"`
		} `json:"user"`
		Head struct {
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	headers, err := p.request(ctx, http.MethodGet, "/repos/"+repository+"/pulls", url.Values{"state": {queryState}, "page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)}}, &payload)
	if err != nil {
		return nil, providers.SourceControlPage{}, err
	}
	items := []providers.SourceControlPullRequest{}
	for _, pull := range payload {
		mappedState := pull.State
		if pull.MergedAt != nil {
			mappedState = "merged"
		}
		if state != "" && state != "all" && mappedState != state {
			continue
		}
		items = append(items, providers.SourceControlPullRequest{Number: pull.Number, Title: pull.Title, State: mappedState, Author: pull.User.Login, SourceBranch: pull.Head.Ref, TargetBranch: pull.Base.Ref, UpdatedAt: pull.Updated, WebURL: pull.HTMLURL})
	}
	return items, providers.SourceControlPage{Page: page, PerPage: perPage, NextPage: githubNextPage(headers)}, nil
}

func (p *githubProvider) GetCommitStatus(ctx context.Context, repository, commitSHA string) (providers.SourceControlCIStatus, error) {
	repository, err := validateRepositoryPath(repository)
	if err != nil {
		return providers.SourceControlCIStatus{}, err
	}
	if !validCommitIdentifier(commitSHA) {
		return providers.SourceControlCIStatus{}, errors.New("invalid commit SHA")
	}
	var actions struct {
		TotalCount int `json:"total_count"`
		Runs       []struct {
			ID          int64      `json:"id"`
			Name        string     `json:"name"`
			RunNumber   int64      `json:"run_number"`
			Status      string     `json:"status"`
			Conclusion  string     `json:"conclusion"`
			HTMLURL     string     `json:"html_url"`
			HeadSHA     string     `json:"head_sha"`
			RunStarted  *time.Time `json:"run_started_at"`
			UpdatedAt   *time.Time `json:"updated_at"`
		} `json:"workflow_runs"`
	}
	_, err = p.request(ctx, http.MethodGet, "/repos/"+repository+"/actions/runs", url.Values{"head_sha": {commitSHA}, "per_page": {"1"}}, &actions)
	if errors.Is(err, ErrPermission) || errors.Is(err, ErrNotFound) {
		return providers.SourceControlCIStatus{Available: false, Provider: "github", CommitSHA: commitSHA, Message: "CI information unavailable for current credentials"}, nil
	}
	if err != nil {
		return providers.SourceControlCIStatus{}, err
	}
	if len(actions.Runs) == 0 {
		return providers.SourceControlCIStatus{Available: false, Provider: "github", CommitSHA: commitSHA, Message: "No GitHub Actions run found for this commit"}, nil
	}
	run := actions.Runs[0]
	status := run.Conclusion
	if status == "" {
		status = run.Status
	}
	var duration int64
	if run.RunStarted != nil && run.UpdatedAt != nil {
		duration = run.UpdatedAt.Sub(*run.RunStarted).Milliseconds()
	}
	return providers.SourceControlCIStatus{Available: true, Provider: "github", Status: status, Name: run.Name, RunNumber: run.RunNumber, CommitSHA: run.HeadSHA, DurationMS: duration, WebURL: run.HTMLURL}, nil
}

func (p *githubProvider) BuildWebURL(repository string, kind string, identifier string) (string, error) {
	repository, err := validateRepositoryPath(repository)
	if err != nil {
		return "", err
	}
	base := strings.TrimRight(p.webURL.String(), "/") + "/" + repository
	switch kind {
	case "repository":
		return base, nil
	case "commit":
		if !validCommitIdentifier(identifier) {
			return "", errors.New("invalid commit identifier")
		}
		return base + "/commit/" + url.PathEscape(identifier), nil
	case "pull_request":
		if _, err := strconv.Atoi(identifier); err != nil {
			return "", errors.New("invalid pull request number")
		}
		return base + "/pull/" + identifier, nil
	case "ci":
		return base + "/actions", nil
	default:
		return "", errors.New("unsupported GitHub web link kind")
	}
}

type gitlabProvider struct{ providerClient }

type gitlabUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	WebURL   string `json:"web_url"`
}

func (p *gitlabProvider) TestConnection(ctx context.Context) (providers.SourceControlUser, error) {
	return p.CurrentUser(ctx)
}

func (p *gitlabProvider) CurrentUser(ctx context.Context) (providers.SourceControlUser, error) {
	var payload gitlabUser
	_, err := p.request(ctx, http.MethodGet, "/user", nil, &payload)
	if err != nil {
		return providers.SourceControlUser{}, err
	}
	return providers.SourceControlUser{ID: strconv.FormatInt(payload.ID, 10), Username: payload.Username, Name: payload.Name, WebURL: payload.WebURL}, nil
}

func (p *gitlabProvider) ListNamespaces(ctx context.Context, search string, page, perPage int) ([]providers.SourceControlNamespace, providers.SourceControlPage, error) {
	page, perPage = pageValues(page, perPage)
	var groups []struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		FullPath string `json:"full_path"`
		WebURL   string `json:"web_url"`
	}
	query := url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)}, "min_access_level": {"10"}, "all_available": {"false"}}
	if search != "" {
		query.Set("search", search)
	}
	headers, err := p.request(ctx, http.MethodGet, "/groups", query, &groups)
	if err != nil {
		return nil, providers.SourceControlPage{}, err
	}
	user, err := p.CurrentUser(ctx)
	if err != nil {
		return nil, providers.SourceControlPage{}, err
	}
	items := []providers.SourceControlNamespace{{ID: user.ID, Name: user.Username, Path: user.Username, Kind: "user", WebURL: user.WebURL}}
	for _, group := range groups {
		items = append(items, providers.SourceControlNamespace{ID: strconv.FormatInt(group.ID, 10), Name: group.Name, Path: group.FullPath, Kind: "group", WebURL: group.WebURL})
	}
	return items, gitlabPage(headers, page, perPage), nil
}

type gitlabRepository struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	PathWithNamespace string `json:"path_with_namespace"`
	HTTPURL            string `json:"http_url_to_repo"`
	SSHURL             string `json:"ssh_url_to_repo"`
	WebURL             string `json:"web_url"`
	DefaultBranch      string `json:"default_branch"`
	Visibility         string `json:"visibility"`
	Namespace          struct {
		ID       int64  `json:"id"`
		FullPath string `json:"full_path"`
		Path     string `json:"path"`
	} `json:"namespace"`
}

func (p *gitlabProvider) ListRepositories(ctx context.Context, namespace, search string, page, perPage int) ([]providers.SourceControlRepository, providers.SourceControlPage, error) {
	page, perPage = pageValues(page, perPage)
	path := "/projects"
	query := url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)}, "order_by": {"last_activity_at"}, "sort": {"desc"}, "simple": {"true"}}
	if namespace == "" {
		query.Set("membership", "true")
	} else {
		path = "/groups/" + url.PathEscape(namespace) + "/projects"
		query.Set("include_subgroups", "true")
	}
	if search != "" {
		query.Set("search", search)
	}
	var payload []gitlabRepository
	headers, err := p.request(ctx, http.MethodGet, path, query, &payload)
	if err != nil {
		if namespace != "" && errors.Is(err, ErrNotFound) {
			path = "/users/" + url.PathEscape(namespace) + "/projects"
			headers, err = p.request(ctx, http.MethodGet, path, query, &payload)
		}
		if err != nil {
			return nil, providers.SourceControlPage{}, err
		}
	}
	items := make([]providers.SourceControlRepository, 0, len(payload))
	for _, repository := range payload {
		items = append(items, mapGitLabRepository(repository))
	}
	return items, gitlabPage(headers, page, perPage), nil
}

func (p *gitlabProvider) GetRepository(ctx context.Context, repository string) (providers.SourceControlRepository, error) {
	repository, err := validateRepositoryPath(repository)
	if err != nil {
		return providers.SourceControlRepository{}, err
	}
	var payload gitlabRepository
	_, err = p.request(ctx, http.MethodGet, "/projects/"+url.PathEscape(repository), nil, &payload)
	if err != nil {
		return providers.SourceControlRepository{}, err
	}
	return mapGitLabRepository(payload), nil
}

func mapGitLabRepository(repository gitlabRepository) providers.SourceControlRepository {
	owner := repository.Namespace.FullPath
	if owner == "" {
		owner = repository.Namespace.Path
	}
	return providers.SourceControlRepository{
		ID: strconv.FormatInt(repository.ID, 10), Owner: owner, Path: repository.PathWithNamespace, Name: repository.Name,
		CloneURL: repository.HTTPURL, SSHURL: repository.SSHURL, WebURL: repository.WebURL, DefaultBranch: repository.DefaultBranch,
		Visibility: repository.Visibility, Namespace: owner,
	}
}

func (p *gitlabProvider) ListBranches(ctx context.Context, repository, search string, page, perPage int) ([]providers.SourceControlBranch, providers.SourceControlPage, error) {
	repository, err := validateRepositoryPath(repository)
	if err != nil {
		return nil, providers.SourceControlPage{}, err
	}
	page, perPage = pageValues(page, perPage)
	query := url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)}}
	if search != "" {
		query.Set("search", search)
	}
	var payload []struct {
		Name      string `json:"name"`
		Default   bool   `json:"default"`
		Protected bool   `json:"protected"`
		Commit    struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	headers, err := p.request(ctx, http.MethodGet, "/projects/"+url.PathEscape(repository)+"/repository/branches", query, &payload)
	if err != nil {
		return nil, providers.SourceControlPage{}, err
	}
	items := make([]providers.SourceControlBranch, 0, len(payload))
	for _, branch := range payload {
		items = append(items, providers.SourceControlBranch{Name: branch.Name, CommitSHA: branch.Commit.ID, Default: branch.Default, Protected: branch.Protected})
	}
	return items, gitlabPage(headers, page, perPage), nil
}

func (p *gitlabProvider) ListTags(ctx context.Context, repository string, page, perPage int) ([]providers.SourceControlTag, providers.SourceControlPage, error) {
	repository, err := validateRepositoryPath(repository)
	if err != nil {
		return nil, providers.SourceControlPage{}, err
	}
	page, perPage = pageValues(page, perPage)
	var payload []struct {
		Name   string `json:"name"`
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	headers, err := p.request(ctx, http.MethodGet, "/projects/"+url.PathEscape(repository)+"/repository/tags", url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)}}, &payload)
	if err != nil {
		return nil, providers.SourceControlPage{}, err
	}
	items := make([]providers.SourceControlTag, 0, len(payload))
	for _, tag := range payload {
		items = append(items, providers.SourceControlTag{Name: tag.Name, CommitSHA: tag.Commit.ID})
	}
	return items, gitlabPage(headers, page, perPage), nil
}

func (p *gitlabProvider) ListPullRequests(ctx context.Context, repository, state string, page, perPage int) ([]providers.SourceControlPullRequest, providers.SourceControlPage, error) {
	repository, err := validateRepositoryPath(repository)
	if err != nil {
		return nil, providers.SourceControlPage{}, err
	}
	page, perPage = pageValues(page, perPage)
	query := url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)}, "scope": {"all"}}
	switch state {
	case "open":
		query.Set("state", "opened")
	case "closed", "merged":
		query.Set("state", state)
	default:
		query.Set("state", "all")
	}
	var payload []struct {
		IID          int       `json:"iid"`
		Title        string    `json:"title"`
		State        string    `json:"state"`
		SourceBranch string    `json:"source_branch"`
		TargetBranch string    `json:"target_branch"`
		UpdatedAt    time.Time `json:"updated_at"`
		WebURL       string    `json:"web_url"`
		Author       struct {
			Username string `json:"username"`
		} `json:"author"`
	}
	headers, err := p.request(ctx, http.MethodGet, "/projects/"+url.PathEscape(repository)+"/merge_requests", query, &payload)
	if err != nil {
		return nil, providers.SourceControlPage{}, err
	}
	items := make([]providers.SourceControlPullRequest, 0, len(payload))
	for _, merge := range payload {
		mappedState := merge.State
		if mappedState == "opened" {
			mappedState = "open"
		}
		items = append(items, providers.SourceControlPullRequest{Number: merge.IID, Title: merge.Title, State: mappedState, Author: merge.Author.Username, SourceBranch: merge.SourceBranch, TargetBranch: merge.TargetBranch, UpdatedAt: merge.UpdatedAt, WebURL: merge.WebURL})
	}
	return items, gitlabPage(headers, page, perPage), nil
}

func (p *gitlabProvider) GetCommitStatus(ctx context.Context, repository, commitSHA string) (providers.SourceControlCIStatus, error) {
	repository, err := validateRepositoryPath(repository)
	if err != nil {
		return providers.SourceControlCIStatus{}, err
	}
	if !validCommitIdentifier(commitSHA) {
		return providers.SourceControlCIStatus{}, errors.New("invalid commit SHA")
	}
	var payload []struct {
		ID        int64      `json:"id"`
		IID       int64      `json:"iid"`
		SHA       string     `json:"sha"`
		Status    string     `json:"status"`
		WebURL    string     `json:"web_url"`
		CreatedAt *time.Time `json:"created_at"`
		UpdatedAt *time.Time `json:"updated_at"`
	}
	_, err = p.request(ctx, http.MethodGet, "/projects/"+url.PathEscape(repository)+"/pipelines", url.Values{"sha": {commitSHA}, "per_page": {"1"}, "order_by": {"id"}, "sort": {"desc"}}, &payload)
	if errors.Is(err, ErrPermission) || errors.Is(err, ErrNotFound) {
		return providers.SourceControlCIStatus{Available: false, Provider: "gitlab", CommitSHA: commitSHA, Message: "CI information unavailable for current credentials"}, nil
	}
	if err != nil {
		return providers.SourceControlCIStatus{}, err
	}
	if len(payload) == 0 {
		return providers.SourceControlCIStatus{Available: false, Provider: "gitlab", CommitSHA: commitSHA, Message: "No GitLab pipeline found for this commit"}, nil
	}
	pipeline := payload[0]
	var duration int64
	if pipeline.CreatedAt != nil && pipeline.UpdatedAt != nil {
		duration = pipeline.UpdatedAt.Sub(*pipeline.CreatedAt).Milliseconds()
	}
	return providers.SourceControlCIStatus{Available: true, Provider: "gitlab", Status: pipeline.Status, Name: "Pipeline", RunNumber: pipeline.IID, CommitSHA: pipeline.SHA, DurationMS: duration, WebURL: pipeline.WebURL}, nil
}

func (p *gitlabProvider) BuildWebURL(repository string, kind string, identifier string) (string, error) {
	repository, err := validateRepositoryPath(repository)
	if err != nil {
		return "", err
	}
	base := strings.TrimRight(p.webURL.String(), "/") + "/" + repository
	switch kind {
	case "repository":
		return base, nil
	case "commit":
		if !validCommitIdentifier(identifier) {
			return "", errors.New("invalid commit identifier")
		}
		return base + "/-/commit/" + url.PathEscape(identifier), nil
	case "pull_request":
		if _, err := strconv.Atoi(identifier); err != nil {
			return "", errors.New("invalid merge request IID")
		}
		return base + "/-/merge_requests/" + identifier, nil
	case "ci":
		return base + "/-/pipelines", nil
	default:
		return "", errors.New("unsupported GitLab web link kind")
	}
}

func gitlabPage(headers http.Header, page, perPage int) providers.SourceControlPage {
	next, _ := strconv.Atoi(headers.Get("X-Next-Page"))
	total, _ := strconv.Atoi(headers.Get("X-Total"))
	return providers.SourceControlPage{Page: page, PerPage: perPage, NextPage: next, TotalCount: total}
}

func validateRepositoryPath(value string) (string, error) {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "" || strings.ContainsAny(value, "\r\n\x00\\") {
		return "", errors.New("invalid repository path")
	}
	parts := strings.Split(value, "/")
	if len(parts) < 2 {
		return "", errors.New("repository path must include namespace and repository")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("invalid repository path")
		}
	}
	return value, nil
}

func validCommitIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 7 || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return false
		}
	}
	return true
}

func splitHeaderList(value string) []string {
	items := []string{}
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}
