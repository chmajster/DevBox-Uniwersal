package sourcecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

func TestGitHubProviderPaginationAndMapping(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			w.Header().Set("X-OAuth-Scopes", "repo, read:org")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "login": "devbox-user", "name": "DevBox User", "html_url": server.URL + "/devbox-user"})
		case "/user/repos":
			page := r.URL.Query().Get("page")
			if page == "1" {
				w.Header().Set("Link", fmt.Sprintf("<%s/user/repos?page=2&per_page=1>; rel=\"next\"", server.URL))
				_ = json.NewEncoder(w).Encode([]map[string]any{githubRepoFixture(11, "acme/app", server.URL)})
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{githubRepoFixture(12, "acme/api", server.URL)})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider, err := NewProvider("github", server.URL, server.URL, "token-value", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	user, err := provider.TestConnection(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if user.Username != "devbox-user" || len(user.Scopes) != 2 {
		t.Fatalf("unexpected user: %#v", user)
	}

	first, page, err := provider.ListRepositories(context.Background(), "", "", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].Path != "acme/app" || first[0].DefaultBranch != "main" || page.NextPage != 2 {
		t.Fatalf("page 1 mapping = %#v page=%#v", first, page)
	}
	second, page, err := provider.ListRepositories(context.Background(), "", "", 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].Path != "acme/api" || page.NextPage != 0 {
		t.Fatalf("page 2 mapping = %#v page=%#v", second, page)
	}
}

func TestGitHubProviderPullRequestAndCI(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"number": 42, "title": "Runtime support", "state": "closed", "merged_at": now,
				"updated_at": now, "html_url": "https://example.invalid/acme/app/pull/42",
				"user": map[string]any{"login": "author"},
				"head": map[string]any{"ref": "feature/runtime"},
				"base": map[string]any{"ref": "main"},
			}})
		case "/repos/acme/app/actions/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 1,
				"workflow_runs": []map[string]any{{
					"id": 1, "name": "CI", "run_number": 152, "status": "completed", "conclusion": "success",
					"html_url": "https://example.invalid/actions/152", "head_sha": "abcdef1234567",
					"run_started_at": now.Add(-time.Minute), "updated_at": now,
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	provider, err := NewProvider("github", server.URL, server.URL, "token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	pulls, _, err := provider.ListPullRequests(context.Background(), "acme/app", "merged", 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(pulls) != 1 || pulls[0].State != "merged" || pulls[0].Number != 42 {
		t.Fatalf("pull mapping = %#v", pulls)
	}
	ci, err := provider.GetCommitStatus(context.Background(), "acme/app", "abcdef1234567")
	if err != nil {
		t.Fatal(err)
	}
	if !ci.Available || ci.Status != "success" || ci.RunNumber != 152 {
		t.Fatalf("CI mapping = %#v", ci)
	}
}

func TestProviderAuthenticationAndRateLimitErrors(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer authServer.Close()
	provider, err := NewProvider("github", authServer.URL, authServer.URL, "bad-token", authServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.CurrentUser(context.Background()); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("auth error = %v, want ErrAuthentication", err)
	}

	reset := time.Now().Add(time.Hour).Unix()
	rateServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
		w.WriteHeader(http.StatusForbidden)
	}))
	defer rateServer.Close()
	provider, err = NewProvider("github", rateServer.URL, rateServer.URL, "token", rateServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.CurrentUser(context.Background())
	var rateErr *RateLimitError
	if !errors.As(err, &rateErr) || rateErr.Remaining != 0 || rateErr.ResetAt.IsZero() {
		t.Fatalf("rate-limit error = %#v", err)
	}
}

func TestGitHubEnterpriseAndGitLabSelfHostedWebURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/user":
			if r.Header.Get("PRIVATE-TOKEN") != "" {
				_ = json.NewEncoder(w).Encode(map[string]any{"id": 9, "username": "gitlab-user", "name": "GitLab User", "web_url": serverURL(r) + "/gitlab-user"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 8, "login": "github-user", "name": "GitHub User", "html_url": serverURL(r) + "/github-user"})
		case r.URL.Path == "/projects":
			w.Header().Set("X-Next-Page", "2")
			w.Header().Set("X-Total", "101")
			_ = json.NewEncoder(w).Encode([]map[string]any{gitLabRepoFixture(33, "group/subgroup/app", serverURL(r))})
		case strings.Contains(r.URL.EscapedPath(), "/projects/group%2Fsubgroup%2Fapp/merge_requests"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"iid": 4, "title": "Merge runtime", "state": "opened", "source_branch": "runtime", "target_branch": "main",
				"updated_at": time.Now().UTC(), "web_url": serverURL(r) + "/group/subgroup/app/-/merge_requests/4",
				"author": map[string]any{"username": "author"},
			}})
		case strings.Contains(r.URL.EscapedPath(), "/projects/group%2Fsubgroup%2Fapp/pipelines"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id": 77, "iid": 77, "sha": "abcdef1234567", "status": "success", "web_url": serverURL(r) + "/pipeline/77",
				"created_at": time.Now().Add(-time.Minute).UTC(), "updated_at": time.Now().UTC(),
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	github, err := NewProvider("github", server.URL, server.URL, "github-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	commitURL, err := github.BuildWebURL("company/platform", "commit", "abcdef1234567")
	if err != nil || commitURL != server.URL+"/company/platform/commit/abcdef1234567" {
		t.Fatalf("GitHub Enterprise URL = %q, %v", commitURL, err)
	}

	gitlab, err := NewProvider("gitlab", server.URL, server.URL, "gitlab-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	repos, page, err := gitlab.ListRepositories(context.Background(), "", "", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Path != "group/subgroup/app" || page.NextPage != 2 || page.TotalCount != 101 {
		t.Fatalf("GitLab project mapping = %#v page=%#v", repos, page)
	}
	mergeURL, err := gitlab.BuildWebURL("group/subgroup/app", "pull_request", "4")
	if err != nil || mergeURL != server.URL+"/group/subgroup/app/-/merge_requests/4" {
		t.Fatalf("GitLab self-hosted URL = %q, %v", mergeURL, err)
	}
}

func TestProviderRejectsInsecureRemoteBaseURL(t *testing.T) {
	_, err := NewProvider("github", "http://example.com", "http://example.com/api/v3", "token", nil)
	if err == nil {
		t.Fatal("expected HTTP remote URL to be rejected")
	}
}

func TestIntegrationJSONDoesNotContainSecret(t *testing.T) {
	payload, err := json.Marshal(Integration{
		ID: "integration-1", Name: "Company GitHub", Provider: "github", CredentialID: "credential-1",
		WebURL: "https://github.example", APIURL: "https://github.example/api/v3", Status: "connected",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if strings.Contains(strings.ToLower(text), "token-value") || strings.Contains(strings.ToLower(text), "secret") {
		t.Fatalf("integration response leaked a secret-bearing field: %s", text)
	}
}

func githubRepoFixture(id int64, fullName, base string) map[string]any {
	parts := strings.SplitN(fullName, "/", 2)
	return map[string]any{
		"id": id, "name": parts[1], "full_name": fullName, "clone_url": base + "/" + fullName + ".git",
		"ssh_url": "git@example.invalid:" + fullName + ".git", "html_url": base + "/" + fullName,
		"default_branch": "main", "visibility": "private", "private": true, "owner": map[string]any{"login": parts[0]},
	}
}

func gitLabRepoFixture(id int64, path, base string) map[string]any {
	parts := strings.Split(path, "/")
	namespace := strings.Join(parts[:len(parts)-1], "/")
	return map[string]any{
		"id": id, "name": parts[len(parts)-1], "path_with_namespace": path,
		"http_url_to_repo": base + "/" + path + ".git", "ssh_url_to_repo": "git@example.invalid:" + path + ".git",
		"web_url": base + "/" + path, "default_branch": "main", "visibility": "private",
		"namespace": map[string]any{"id": 1, "full_path": namespace, "path": namespace},
	}
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}

var _ providers.SourceControlIntegrationProvider = (*githubProvider)(nil)
var _ providers.SourceControlIntegrationProvider = (*gitlabProvider)(nil)
