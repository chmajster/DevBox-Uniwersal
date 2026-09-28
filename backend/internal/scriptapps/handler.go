package scriptapps

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type jobLogger interface {
	Log(context.Context, string, string, string, map[string]any) error
}

type Handler struct {
	typ    string
	repo   *Repository
	log    jobLogger
	client *http.Client
}

func NewHandler(typ string, repo *Repository, logger jobLogger) *Handler {
	return &Handler{typ: typ, repo: repo, log: logger, client: &http.Client{Timeout: 2 * time.Minute}}
}
func (h *Handler) Type() string { return h.typ }

func (h *Handler) Run(ctx context.Context, job domain.Job) (map[string]any, error) {
	id, _ := job.Payload["script_app_id"].(string)
	if id == "" {
		return nil, fmt.Errorf("script_app_id is required")
	}
	a, err := h.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	switch h.typ {
	case JobInstall:
		return h.runScript(ctx, job, a, a.InstallSource, "installing", "installed", true)
	case JobUpdate:
		return h.runScript(ctx, job, a, a.UpdateSource, "updating", "installed", false)
	case JobUninstall:
		return h.runScript(ctx, job, a, a.UninstallSource, "uninstalling", "uninstalled", false)
	case JobStart, JobStop, JobRestart:
		return h.lifecycle(ctx, job, a)
	default:
		return nil, fmt.Errorf("unsupported script app job type %q", h.typ)
	}
}

func (h *Handler) runScript(ctx context.Context, job domain.Job, a App, source, pending, success string, discover bool) (map[string]any, error) {
	if source == "" {
		return nil, fmt.Errorf("script source is not configured")
	}
	beforeServices := setOf(commandLines(ctx, "systemctl", "list-unit-files", "--type=service", "--no-legend", "--no-pager"))
	beforeContainers := setOf(commandLines(ctx, "docker", "ps", "-a", "--format", "{{.Names}}"))
	if err := h.repo.SetState(ctx, a.ID, pending, a.Manager, a.ManagerTarget, ""); err != nil {
		return nil, err
	}
	_ = h.log.Log(ctx, job.ID, "info", "script_app.download", map[string]any{"url": source})
	path, hash, err := h.download(ctx, source)
	if err != nil {
		_ = h.repo.SetState(context.Background(), a.ID, "failed", a.Manager, a.ManagerTarget, err.Error())
		return nil, err
	}
	defer os.Remove(path)
	if a.ChecksumSHA256 != "" && h.typ == JobInstall && hash != a.ChecksumSHA256 {
		err = fmt.Errorf("SHA-256 mismatch: expected %s, got %s", a.ChecksumSHA256, hash)
		_ = h.repo.SetState(context.Background(), a.ID, "failed", a.Manager, a.ManagerTarget, err.Error())
		return nil, err
	}
	args := []string{path}
	binary := a.Interpreter
	if a.RunAsRoot {
		args = append([]string{"-n", binary}, args...)
		binary = "sudo"
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	var out bytes.Buffer
	cmd.Stdout = &limitWriter{w: &out, n: 1024 * 1024}
	cmd.Stderr = &limitWriter{w: &out, n: 1024 * 1024}
	_ = h.log.Log(ctx, job.ID, "info", "script_app.execute", map[string]any{"interpreter": a.Interpreter, "run_as_root": a.RunAsRoot})
	if err = cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if msg != "" {
			_ = h.log.Log(context.Background(), job.ID, "error", "script_app.output", map[string]any{"output": msg})
		}
		wrapped := fmt.Errorf("installer failed: %w", err)
		_ = h.repo.SetState(context.Background(), a.ID, "failed", a.Manager, a.ManagerTarget, wrapped.Error())
		return nil, wrapped
	}
	if text := strings.TrimSpace(out.String()); text != "" {
		_ = h.log.Log(ctx, job.ID, "info", "script_app.output", map[string]any{"output": text})
	}
	manager, target := a.Manager, a.ManagerTarget
	if discover && target == "" {
		afterServices := setOf(commandLines(ctx, "systemctl", "list-unit-files", "--type=service", "--no-legend", "--no-pager"))
		afterContainers := setOf(commandLines(ctx, "docker", "ps", "-a", "--format", "{{.Names}}"))
		if added := diff(afterServices, beforeServices); len(added) == 1 {
			manager, target = "systemd", strings.Fields(added[0])[0]
		}
		if target == "" {
			if added := diff(afterContainers, beforeContainers); len(added) == 1 {
				manager, target = "docker", strings.TrimSpace(added[0])
			}
		}
	}
	if h.typ == JobUninstall {
		manager, target = "script", ""
	}
	if err := h.repo.SetState(ctx, a.ID, success, manager, target, ""); err != nil {
		return nil, err
	}
	return map[string]any{"script_app_id": a.ID, "status": success, "sha256": hash, "manager": manager, "manager_target": target}, nil
}

func (h *Handler) lifecycle(ctx context.Context, job domain.Job, a App) (map[string]any, error) {
	action := strings.TrimPrefix(h.typ, "script_app_")
	var cmd *exec.Cmd
	switch a.Manager {
	case "systemd":
		args := []string{action, a.ManagerTarget}
		if a.RunAsRoot {
			args = append([]string{"-n", "systemctl"}, args...)
			cmd = exec.CommandContext(ctx, "sudo", args...)
		} else {
			cmd = exec.CommandContext(ctx, "systemctl", args...)
		}
	case "docker":
		cmd = exec.CommandContext(ctx, "docker", action, a.ManagerTarget)
	default:
		return nil, fmt.Errorf("application manager %q does not support lifecycle operations", a.Manager)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		_ = h.repo.SetState(context.Background(), a.ID, "failed", a.Manager, a.ManagerTarget, msg)
		return nil, fmt.Errorf("%s failed: %w: %s", action, err, msg)
	}
	status := "installed"
	if action == "start" || action == "restart" {
		status = "running"
	} else if action == "stop" {
		status = "stopped"
	}
	_ = h.repo.SetState(ctx, a.ID, status, a.Manager, a.ManagerTarget, "")
	_ = h.log.Log(ctx, job.ID, "info", "script_app.lifecycle", map[string]any{"action": action, "manager": a.Manager, "target": a.ManagerTarget})
	return map[string]any{"script_app_id": a.ID, "status": status}, nil
}

func (h *Handler) download(ctx context.Context, source string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}
	f, err := os.CreateTemp("", "devbox-script-app-*")
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, 5*1024*1024+1))
	if err != nil {
		os.Remove(f.Name())
		return "", "", err
	}
	if n > 5*1024*1024 {
		os.Remove(f.Name())
		return "", "", fmt.Errorf("installer exceeds 5 MiB limit")
	}
	if err := f.Chmod(0o700); err != nil {
		os.Remove(f.Name())
		return "", "", err
	}
	return f.Name(), fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func commandLines(ctx context.Context, name string, args ...string) []string {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return nil
	}
	s := bufio.NewScanner(bytes.NewReader(out))
	v := make([]string, 0)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line != "" {
			v = append(v, line)
		}
	}
	return v
}
func setOf(lines []string) map[string]struct{} {
	m := map[string]struct{}{}
	for _, line := range lines {
		m[line] = struct{}{}
	}
	return m
}
func diff(after, before map[string]struct{}) []string {
	out := make([]string, 0)
	for k := range after {
		if _, ok := before[k]; !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

type limitWriter struct {
	w io.Writer
	n int64
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return len(p), nil
	}
	if int64(len(p)) > l.n {
		_, err := l.w.Write(p[:l.n])
		l.n = 0
		return len(p), err
	}
	n, err := l.w.Write(p)
	l.n -= int64(n)
	return n, err
}
