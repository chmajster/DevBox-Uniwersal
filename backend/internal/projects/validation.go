package projects

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var branchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$`)
var scpRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[A-Za-z0-9._/-]+$`)

func ValidateRepositoryURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("repository URL is required")
	}
	if strings.ContainsAny(raw, "\r\n\x00") {
		return errors.New("repository URL contains invalid characters")
	}
	if scpRepositoryPattern.MatchString(raw) {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errors.New("repository URL must use https, ssh or git")
	}
	scheme := strings.ToLower(u.Scheme)
	if u.User != nil {
		_, hasPassword := u.User.Password()
		if scheme != "ssh" || hasPassword || u.User.Username() == "" {
			return errors.New("repository URL must not contain embedded credentials")
		}
	}
	switch scheme {
	case "https", "ssh", "git":
		return nil
	default:
		return errors.New("repository URL must use https, ssh or git")
	}
}

func ValidateBranch(branch string) error {
	branch = strings.TrimSpace(branch)
	if !branchPattern.MatchString(branch) || strings.Contains(branch, "..") || strings.Contains(branch, "@{") || strings.Contains(branch, "//") || strings.HasSuffix(branch, "/") || strings.HasSuffix(branch, ".") || strings.HasSuffix(branch, ".lock") {
		return errors.New("invalid Git branch")
	}
	return nil
}

func ValidateExistingDirectory(path string) (string, error) {
	if strings.TrimSpace(path) == "" || strings.ContainsRune(path, '\x00') {
		return "", errors.New("local path is required")
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("local path must be absolute")
	}
	clean := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", fmt.Errorf("resolve local path: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat local path: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("local path is not a directory")
	}
	return resolved, nil
}

func SafeProjectPath(root, slug string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", errors.New("projects root is not configured")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve projects root: %w", err)
	}
	candidate := filepath.Join(absRoot, slug)
	rel, err := filepath.Rel(absRoot, candidate)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", errors.New("project path escapes projects root")
	}
	return candidate, nil
}

func Slugify(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func validateCredentialKind(kind string) error {
	switch kind {
	case "", "token", "ssh_key":
		return nil
	default:
		return errors.New("credential_kind must be token or ssh_key")
	}
}

func SafeWorkingDirectory(base, relative string) (string, error) {
	if relative == "" || relative == "." {
		return base, nil
	}
	if filepath.IsAbs(relative) || strings.ContainsRune(relative, '\x00') {
		return "", errors.New("working directory must be relative")
	}
	base = filepath.Clean(base)
	candidate := filepath.Clean(filepath.Join(base, relative))
	rel, err := filepath.Rel(base, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("working directory escapes project path")
	}
	return candidate, nil
}
