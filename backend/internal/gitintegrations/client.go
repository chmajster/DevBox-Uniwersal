package gitintegrations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type Identity struct {
	Username             string   `json:"username"`
	Scopes               []string `json:"scopes"`
	ScopeNote            string   `json:"scope_note"`
	RepositoriesReadable bool     `json:"repositories_readable"`
}
type Repository struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	CloneURL      string `json:"clone_url"`
	URL           string `json:"url"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}
type Branch struct {
	Name      string `json:"name"`
	SHA       string `json:"sha"`
	Protected bool   `json:"protected"`
}
type Activity struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	URL    string `json:"url"`
	Branch string `json:"branch"`
}
type Overview struct {
	Requests  []Activity `json:"requests"`
	Pipelines []Activity `json:"pipelines"`
	Warnings  []string   `json:"warnings"`
}

func (i Integration) apiBase() string {
	if i.Provider == "gitlab" {
		return i.BaseURL + "/api/v4"
	}
	return i.BaseURL
}
func (s *Service) get(ctx context.Context, i Integration, path string, target any) (http.Header, error) {
	token, err := s.credentials.ReadToken(ctx, i.CredentialID)
	if err != nil {
		return nil, err
	}
	defer clear(token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.apiBase()+path, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid integration request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "DevBox-Universal")
	if i.Provider == "github" {
		req.Header.Set("Authorization", "Bearer "+string(token))
		req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
		req.Header.Set("Accept", "application/vnd.github+json")
	} else {
		req.Header.Set("PRIVATE-TOKEN", string(token))
	}
	response, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Git provider connection failed; check DNS, TLS and configured server")
	}
	defer response.Body.Close()
	// Do not echo provider response bodies, URLs with query arguments, or tokens.
	if response.StatusCode != http.StatusOK {
		return response.Header, fmt.Errorf("Git provider returned HTTP %d; verify token permissions and resource access", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read provider response")
	}
	if len(data) > 4*1024*1024 {
		return nil, fmt.Errorf("Git provider response exceeds limit")
	}
	if err = json.Unmarshal(data, target); err != nil {
		return nil, fmt.Errorf("invalid Git provider JSON")
	}
	return response.Header, nil
}
func (s *Service) Test(ctx context.Context, i Integration) (Identity, error) {
	var user struct {
		Login    string `json:"login"`
		Username string `json:"username"`
	}
	headers, err := s.get(ctx, i, "/user", &user)
	if err != nil {
		return Identity{}, err
	}
	identity := Identity{Username: user.Login, Scopes: []string{}, ScopeNote: "Fine-grained and project token scopes may not be enumerated by the provider; successful requests verify only the exercised capabilities."}
	if i.Provider == "gitlab" {
		identity.Username = user.Username
	}
	for _, scope := range strings.Split(headers.Get("X-OAuth-Scopes"), ",") {
		if scope = strings.TrimSpace(scope); scope != "" {
			identity.Scopes = append(identity.Scopes, scope)
		}
	}
	if identity.Username == "" {
		return identity, fmt.Errorf("provider did not return an authenticated identity")
	}
	_, err = s.Repositories(ctx, i, 1)
	identity.RepositoriesReadable = err == nil
	if err != nil {
		return identity, err
	}
	return identity, nil
}
func safeWebURL(raw string, i Integration) string {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil {
		return ""
	}
	expected := i.BaseURL
	if i.Provider == "github" {
		expected = "https://github.com"
	}
	root, _ := url.Parse(expected)
	if !strings.EqualFold(u.Host, root.Host) {
		return ""
	}
	return u.String()
}
func (s *Service) Repositories(ctx context.Context, i Integration, page int) ([]Repository, error) {
	if page < 1 || page > 1000 {
		return nil, fmt.Errorf("page must be 1..1000")
	}
	out := []Repository{}
	if i.Provider == "github" {
		var raw []struct {
			FullName      string `json:"full_name"`
			CloneURL      string `json:"clone_url"`
			URL           string `json:"html_url"`
			DefaultBranch string `json:"default_branch"`
			Private       bool   `json:"private"`
		}
		_, err := s.get(ctx, i, "/user/repos?per_page=100&sort=updated&page="+strconv.Itoa(page), &raw)
		if err != nil {
			return nil, err
		}
		for _, r := range raw {
			clone := safeWebURL(r.CloneURL, i)
			if clone == "" {
				continue
			}
			out = append(out, Repository{ID: r.FullName, Name: r.FullName, CloneURL: clone, URL: safeWebURL(r.URL, i), DefaultBranch: r.DefaultBranch, Private: r.Private})
		}
	} else {
		var raw []struct {
			ID            int64  `json:"id"`
			Name          string `json:"path_with_namespace"`
			CloneURL      string `json:"http_url_to_repo"`
			URL           string `json:"web_url"`
			DefaultBranch string `json:"default_branch"`
			Visibility    string `json:"visibility"`
		}
		_, err := s.get(ctx, i, "/projects?membership=true&simple=true&order_by=last_activity_at&sort=desc&per_page=100&page="+strconv.Itoa(page), &raw)
		if err != nil {
			return nil, err
		}
		for _, r := range raw {
			clone := safeWebURL(r.CloneURL, i)
			if clone == "" {
				continue
			}
			out = append(out, Repository{ID: strconv.FormatInt(r.ID, 10), Name: r.Name, CloneURL: clone, URL: safeWebURL(r.URL, i), DefaultBranch: r.DefaultBranch, Private: r.Visibility != "public"})
		}
	}
	return out, nil
}

var githubRepository = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func repoPath(i Integration, repo string) (string, error) {
	if len(repo) < 1 || len(repo) > 500 || strings.ContainsAny(repo, "\r\n\x00") {
		return "", fmt.Errorf("invalid repository ID")
	}
	if i.Provider == "github" {
		if !githubRepository.MatchString(repo) {
			return "", fmt.Errorf("GitHub repository must be owner/name")
		}
		for _, part := range strings.Split(repo, "/") {
			if part == "." || part == ".." {
				return "", fmt.Errorf("invalid repository ID")
			}
		}
		return "/repos/" + repo, nil
	}
	return "/projects/" + url.PathEscape(repo), nil
}
func (s *Service) Branches(ctx context.Context, i Integration, repo string, page int) ([]Branch, error) {
	if page < 1 || page > 1000 {
		return nil, fmt.Errorf("invalid page")
	}
	prefix, err := repoPath(i, repo)
	if err != nil {
		return nil, err
	}
	endpoint := prefix + "/branches"
	if i.Provider == "gitlab" {
		endpoint = prefix + "/repository/branches"
	}
	var raw []struct {
		Name      string `json:"name"`
		Protected bool   `json:"protected"`
		Commit    struct {
			SHA string `json:"sha"`
			ID  string `json:"id"`
		} `json:"commit"`
	}
	_, err = s.get(ctx, i, endpoint+"?per_page=100&page="+strconv.Itoa(page), &raw)
	if err != nil {
		return nil, err
	}
	out := []Branch{}
	for _, b := range raw {
		sha := b.Commit.SHA
		if sha == "" {
			sha = b.Commit.ID
		}
		out = append(out, Branch{Name: b.Name, SHA: sha, Protected: b.Protected})
	}
	return out, nil
}
func (s *Service) Overview(ctx context.Context, i Integration, repo, branch string) (Overview, error) {
	prefix, err := repoPath(i, repo)
	if err != nil {
		return Overview{}, err
	}
	if len(branch) > 512 {
		return Overview{}, fmt.Errorf("branch is too long")
	}
	out := Overview{Requests: []Activity{}, Pipelines: []Activity{}, Warnings: []string{}}
	if i.Provider == "github" {
		var prs []struct {
			Number int64  `json:"number"`
			Title  string `json:"title"`
			State  string `json:"state"`
			URL    string `json:"html_url"`
			Head   struct {
				Ref string `json:"ref"`
			} `json:"head"`
		}
		if _, err = s.get(ctx, i, prefix+"/pulls?state=open&per_page=20", &prs); err != nil {
			out.Warnings = append(out.Warnings, "Pull requests: "+err.Error())
		} else {
			for _, p := range prs {
				out.Requests = append(out.Requests, Activity{ID: p.Number, Title: p.Title, Status: p.State, URL: safeWebURL(p.URL, i), Branch: p.Head.Ref})
			}
		}
		var runs struct {
			Runs []struct {
				ID         int64  `json:"id"`
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				URL        string `json:"html_url"`
				Branch     string `json:"head_branch"`
			} `json:"workflow_runs"`
		}
		endpoint := prefix + "/actions/runs?per_page=20"
		if branch != "" {
			endpoint += "&branch=" + url.QueryEscape(branch)
		}
		if _, err = s.get(ctx, i, endpoint, &runs); err != nil {
			out.Warnings = append(out.Warnings, "Actions: "+err.Error())
		} else {
			for _, p := range runs.Runs {
				status := p.Status
				if p.Conclusion != "" {
					status = p.Conclusion
				}
				out.Pipelines = append(out.Pipelines, Activity{ID: p.ID, Title: p.Name, Status: status, URL: safeWebURL(p.URL, i), Branch: p.Branch})
			}
		}
	} else {
		var mrs []struct {
			IID    int64  `json:"iid"`
			Title  string `json:"title"`
			State  string `json:"state"`
			URL    string `json:"web_url"`
			Branch string `json:"source_branch"`
		}
		if _, err = s.get(ctx, i, prefix+"/merge_requests?state=opened&per_page=20", &mrs); err != nil {
			out.Warnings = append(out.Warnings, "Merge requests: "+err.Error())
		} else {
			for _, p := range mrs {
				out.Requests = append(out.Requests, Activity{ID: p.IID, Title: p.Title, Status: p.State, URL: safeWebURL(p.URL, i), Branch: p.Branch})
			}
		}
		var runs []struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
			URL    string `json:"web_url"`
			Branch string `json:"ref"`
		}
		endpoint := prefix + "/pipelines?per_page=20"
		if branch != "" {
			endpoint += "&ref=" + url.QueryEscape(branch)
		}
		if _, err = s.get(ctx, i, endpoint, &runs); err != nil {
			out.Warnings = append(out.Warnings, "Pipelines: "+err.Error())
		} else {
			for _, p := range runs {
				out.Pipelines = append(out.Pipelines, Activity{ID: p.ID, Title: "Pipeline " + strconv.FormatInt(p.ID, 10), Status: p.Status, URL: safeWebURL(p.URL, i), Branch: p.Branch})
			}
		}
	}
	return out, nil
}
