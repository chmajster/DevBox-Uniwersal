package sourcegit

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

var (
	httpCredentialPattern = regexp.MustCompile(`(?i)((?:https?|git)://)[^/@\s]+@`)
	sshPasswordPattern    = regexp.MustCompile(`(?i)(ssh://)[^/@\s:]+:[^/@\s]+@`)
	branchPattern         = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,255}$`)
)

type Client struct{ store secrets.SecretStore }

func New(store secrets.SecretStore) *Client { return &Client{store: store} }

func (c *Client) Clone(ctx context.Context, source providers.GitSource) error {
	if strings.TrimSpace(source.RepositoryURL) == "" || strings.TrimSpace(source.Destination) == "" {
		return errors.New("Git repository URL and destination are required")
	}
	args := []string{"clone"}
	if source.Reference != "" {
		if err := validateReference(source.Reference); err != nil {
			return err
		}
		args = append(args, "--branch", source.Reference, "--single-branch")
	}
	args = append(args, "--", source.RepositoryURL, source.Destination)
	_, err := c.run(ctx, "", source.CredentialRef, args...)
	return err
}
func (c *Client) PullWithCredential(ctx context.Context, workDir string, ref *string) error {
	_, err := c.run(ctx, workDir, ref, "pull", "--ff-only")
	return err
}
func (c *Client) Checkout(ctx context.Context, workDir, reference string) error {
	if err := validateReference(reference); err != nil {
		return err
	}
	if _, err := c.run(ctx, workDir, nil, "switch", reference); err == nil {
		return nil
	}
	_, err := c.run(ctx, workDir, nil, "switch", "--track", "-c", reference, "origin/"+reference)
	return err
}
func (c *Client) Revision(ctx context.Context, workDir string) (string, error) {
	out, err := c.run(ctx, workDir, nil, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}
func (c *Client) IsRepository(ctx context.Context, workDir string) bool {
	out, err := c.run(ctx, workDir, nil, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

func (c *Client) run(ctx context.Context, workDir string, credentialRef *string, args ...string) (string, error) {
	commandDir := strings.TrimSpace(workDir)
	commandArgs := append([]string(nil), args...)
	if commandDir != "" {
		absolute, err := filepath.Abs(commandDir)
		if err != nil {
			return "", err
		}
		if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
			absolute = resolved
		}
		commandDir = absolute
		commandArgs = append([]string{"-c", "safe.directory=" + absolute}, commandArgs...)
	}
	cmd := exec.CommandContext(ctx, "git", commandArgs...)
	if commandDir != "" {
		cmd.Dir = commandDir
	}
	cmd.Env = os.Environ()
	var secret, encoded string
	cleanup := func() {}
	if credentialRef != nil {
		kind, scope, name, err := parseCredentialRef(*credentialRef)
		if err != nil {
			return "", err
		}
		if c.store == nil {
			return "", errors.New("provider unavailable: secret store is not configured")
		}
		plain, err := c.store.Get(ctx, scope, name)
		if err != nil {
			return "", fmt.Errorf("load Git credential: %w", err)
		}
		secret = string(plain)
		switch kind {
		case "token":
			encoded = base64.StdEncoding.EncodeToString([]byte("x-access-token:" + secret))
			cmd.Env = append(cmd.Env, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0=Authorization: Basic "+encoded)
		case "ssh_key":
			file, err := os.CreateTemp("", "devbox-git-key-*")
			if err != nil {
				return "", err
			}
			path := file.Name()
			cleanup = func() { _ = os.Remove(path) }
			if err := file.Chmod(0o600); err != nil {
				_ = file.Close()
				cleanup()
				return "", err
			}
			if _, err := file.Write(plain); err != nil {
				_ = file.Close()
				cleanup()
				return "", err
			}
			if err := file.Close(); err != nil {
				cleanup()
				return "", err
			}
			cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND=ssh -i "+shellQuote(path)+" -o IdentitiesOnly=yes")
		default:
			return "", errors.New("unsupported Git credential kind")
		}
	}
	defer cleanup()
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("git command failed: %s: %w", mask(text, secret, encoded), err)
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
func validateReference(value string) error {
	if !branchPattern.MatchString(value) || strings.Contains(value, "..") || strings.HasPrefix(value, "-") {
		return errors.New("invalid Git reference")
	}
	return nil
}
func mask(input string, values ...string) string {
	out := httpCredentialPattern.ReplaceAllString(input, "$1***@")
	out = sshPasswordPattern.ReplaceAllString(out, "$1***@")
	for _, value := range values {
		if value != "" {
			out = strings.ReplaceAll(out, value, "***")
		}
	}
	return out
}
func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
