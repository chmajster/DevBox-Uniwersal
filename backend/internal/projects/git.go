package projects

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

var httpCredentialPattern = regexp.MustCompile(`(?i)((?:https?|git)://)[^/@\s]+@`)
var sshPasswordPattern = regexp.MustCompile(`(?i)(ssh://)[^/@\s:]+:[^/@\s]+@`)

type GitClient struct {
	store secrets.SecretStore
}

func NewGitClient(store secrets.SecretStore) *GitClient { return &GitClient{store: store} }

func (g *GitClient) Clone(ctx context.Context, source providers.GitSource) error {
	args := []string{"clone"}
	if source.Reference != "" {
		args = append(args, "--branch", source.Reference, "--single-branch")
	}
	args = append(args, source.RepositoryURL, source.Destination)
	_, err := g.run(ctx, "", source.CredentialRef, args...)
	return err
}

func (g *GitClient) Pull(ctx context.Context, workDir string) error {
	_, err := g.run(ctx, workDir, nil, "pull", "--ff-only")
	return err
}

func (g *GitClient) PullWithCredential(ctx context.Context, workDir string, ref *string) error {
	_, err := g.run(ctx, workDir, ref, "pull", "--ff-only")
	return err
}

func (g *GitClient) Fetch(ctx context.Context, workDir string, ref *string) error {
	_, err := g.run(ctx, workDir, ref, "fetch", "--prune", "origin")
	return err
}

func (g *GitClient) Checkout(ctx context.Context, workDir, reference string) error {
	if err := ValidateBranch(reference); err != nil {
		return err
	}
	status, err := g.run(ctx, workDir, nil, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return ErrWorkingTreeDirty
	}
	if _, err := g.run(ctx, workDir, nil, "switch", reference); err == nil {
		return nil
	}
	_, err := g.run(ctx, workDir, nil, "switch", "--track", "-c", reference, "origin/"+reference)
	return err
}

func (g *GitClient) Revision(ctx context.Context, workDir string) (string, error) {
	out, err := g.run(ctx, workDir, nil, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

func (g *GitClient) IsRepository(ctx context.Context, workDir string) bool {
	out, err := g.run(ctx, workDir, nil, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

func (g *GitClient) State(ctx context.Context, workDir string) (GitState, error) {
	if !g.IsRepository(ctx, workDir) {
		return GitState{}, errors.New("provider unavailable: path is not a Git repository")
	}
	branch, err := g.run(ctx, workDir, nil, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return GitState{}, err
	}
	commit, err := g.Revision(ctx, workDir)
	if err != nil {
		return GitState{}, err
	}
	remote, _ := g.run(ctx, workDir, nil, "remote", "get-url", "origin")
	status, err := g.run(ctx, workDir, nil, "status", "--porcelain")
	if err != nil {
		return GitState{}, err
	}
	ahead, behind := 0, 0
	if counts, countErr := g.run(ctx, workDir, nil, "rev-list", "--left-right", "--count", "HEAD...@{upstream}"); countErr == nil {
		ahead, behind = parseAheadBehind(counts)
	}
	branches, err := g.Branches(ctx, workDir)
	if err != nil {
		return GitState{}, err
	}
	history, err := g.History(ctx, workDir, 30)
	if err != nil {
		return GitState{}, err
	}
	return GitState{
		Branch:   strings.TrimSpace(branch),
		Commit:   strings.TrimSpace(commit),
		Remote:   MaskSecrets(strings.TrimSpace(remote)),
		Ahead:    ahead,
		Behind:   behind,
		Dirty:    strings.TrimSpace(status) != "",
		Branches: branches,
		History:  history,
	}, nil
}

func (g *GitClient) Branches(ctx context.Context, workDir string) ([]string, error) {
	out, err := g.run(ctx, workDir, nil, "for-each-ref", "--format=%(refname:short)", "refs/heads", "refs/remotes/origin")
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "origin/HEAD" {
			continue
		}
		line = strings.TrimPrefix(line, "origin/")
		seen[line] = struct{}{}
	}
	branches := make([]string, 0, len(seen))
	for branch := range seen {
		branches = append(branches, branch)
	}
	sort.Strings(branches)
	return branches, nil
}

func (g *GitClient) History(ctx context.Context, workDir string, limit int) ([]GitCommit, error) {
	return g.HistoryPage(ctx, workDir, limit, 0)
}

func (g *GitClient) HistoryPage(ctx context.Context, workDir string, limit, offset int) ([]GitCommit, error) {
	if limit <= 0 || limit > 200 {
		limit = 30
	}
	if offset < 0 {
		offset = 0
	}
	out, err := g.run(ctx, workDir, nil, "log", "-n", strconv.Itoa(limit), "--skip", strconv.Itoa(offset), "--pretty=format:%H%x1f%an%x1f%aI%x1f%s")
	if err != nil {
		return nil, err
	}
	commits := make([]GitCommit, 0, limit)
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\x1f", 4)
		if len(parts) != 4 {
			continue
		}
		when, err := time.Parse(time.RFC3339, parts[2])
		if err != nil {
			continue
		}
		commits = append(commits, GitCommit{Hash: parts[0], Author: parts[1], Date: when, Subject: parts[3]})
	}
	return commits, nil
}

func (g *GitClient) Tags(ctx context.Context, workDir string) ([]string, error) {
	out, err := g.run(ctx, workDir, nil, "tag", "--list", "--sort=-creatordate")
	if err != nil {
		return nil, err
	}
	items := []string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			items = append(items, line)
		}
	}
	return items, nil
}

func parseAheadBehind(raw string) (ahead, behind int) {
	fields := strings.Fields(raw)
	if len(fields) != 2 {
		return 0, 0
	}
	ahead, _ = strconv.Atoi(fields[0])
	behind, _ = strconv.Atoi(fields[1])
	return ahead, behind
}

func CredentialRef(kind, projectID, name string) *string {
	if kind == "" || name == "" {
		return nil
	}
	value := kind + "|git/" + projectID + "|" + name
	return &value
}

func ProjectCredentialRef(project Project) *string {
	if project.CredentialKind == "" {
		return nil
	}
	if project.CredentialID != "" {
		kind := project.CredentialKind
		if kind == "token" && project.SourceControlProvider == "github" {
			kind = "github_token"
		}
		if kind == "token" && project.SourceControlProvider == "gitlab" {
			kind = "gitlab_token"
		}
		value := kind + "|credential/" + project.CredentialID + "|value"
		return &value
	}
	return CredentialRef(project.CredentialKind, project.ID, "default")
}

func (g *GitClient) run(ctx context.Context, workDir string, credentialRef *string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Env = os.Environ()
	var secret string
	var encodedSecret string
	var cleanup func()
	if credentialRef != nil {
		kind, scope, name, err := parseCredentialRef(*credentialRef)
		if err != nil {
			return "", err
		}
		if g.store == nil {
			return "", errors.New("provider unavailable: secret store is not configured")
		}
		plain, err := g.store.Get(ctx, scope, name)
		if err != nil {
			return "", fmt.Errorf("load Git credential: %w", err)
		}
		secret = string(plain)
		switch kind {
		case "token", "github_token":
			basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + secret))
			encodedSecret = basic
			cmd.Env = append(cmd.Env,
				"GIT_CONFIG_COUNT=1",
				"GIT_CONFIG_KEY_0=http.extraHeader",
				"GIT_CONFIG_VALUE_0=Authorization: Basic "+basic,
			)
		case "gitlab_token":
			basic := base64.StdEncoding.EncodeToString([]byte("oauth2:" + secret))
			encodedSecret = basic
			cmd.Env = append(cmd.Env,
				"GIT_CONFIG_COUNT=1",
				"GIT_CONFIG_KEY_0=http.extraHeader",
				"GIT_CONFIG_VALUE_0=Authorization: Basic "+basic,
			)
		case "ssh_key":
			f, err := os.CreateTemp("", "devbox-git-key-*")
			if err != nil {
				return "", fmt.Errorf("create temporary Git key: %w", err)
			}
			path := f.Name()
			cleanup = func() { _ = os.Remove(path) }
			if err := f.Chmod(0o600); err != nil {
				_ = f.Close()
				cleanup()
				return "", err
			}
			if _, err := f.Write(plain); err != nil {
				_ = f.Close()
				cleanup()
				return "", err
			}
			if err := f.Close(); err != nil {
				cleanup()
				return "", err
			}
			cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND=ssh -i "+shellQuote(path)+" -o IdentitiesOnly=yes")
		default:
			return "", errors.New("unsupported Git credential kind")
		}
	}
	if cleanup != nil {
		defer cleanup()
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("git command failed: %s: %w", MaskSecrets(text, secret, encodedSecret), err)
	}
	return text, nil
}

func parseCredentialRef(ref string) (kind, scope, name string, err error) {
	parts := strings.SplitN(ref, "|", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", errors.New("invalid Git credential reference")
	}
	return parts[0], parts[1], parts[2], nil
}

func MaskSecrets(input string, values ...string) string {
	masked := httpCredentialPattern.ReplaceAllString(input, "${1}***@")
	masked = sshPasswordPattern.ReplaceAllString(masked, "${1}***@")
	for _, value := range values {
		if value != "" {
			masked = strings.ReplaceAll(masked, value, "***")
		}
	}
	return masked
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
