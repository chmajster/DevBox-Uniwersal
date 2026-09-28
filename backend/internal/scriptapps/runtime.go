package scriptapps

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func (s *Service) RefreshStatus(ctx context.Context, id string) (App, error) {
	a, err := s.repo.Get(ctx, id)
	if err != nil {
		return App{}, err
	}
	status := a.Status
	switch a.Manager {
	case "systemd":
		if a.ManagerTarget != "" {
			cmd := exec.CommandContext(ctx, "systemctl", "is-active", a.ManagerTarget)
			out, runErr := cmd.CombinedOutput()
			state := strings.TrimSpace(string(out))
			if runErr == nil && state == "active" {
				status = "running"
			} else if state == "inactive" || state == "failed" || state == "activating" || state == "deactivating" {
				status = state
			}
		}
	case "docker":
		if a.ManagerTarget != "" {
			out, runErr := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Status}}", a.ManagerTarget).CombinedOutput()
			if runErr == nil {
				state := strings.TrimSpace(string(out))
				if state != "" {
					status = state
				}
			}
		}
	}
	if status != a.Status {
		_ = s.repo.SetState(ctx, a.ID, status, a.Manager, a.ManagerTarget, a.LastError)
	}
	return s.repo.Get(ctx, id)
}

func (s *Service) Logs(ctx context.Context, id string) (LogsResult, error) {
	a, err := s.repo.Get(ctx, id)
	if err != nil {
		return LogsResult{}, err
	}
	var cmd *exec.Cmd
	switch a.Manager {
	case "systemd":
		if a.ManagerTarget == "" {
			return LogsResult{}, fmt.Errorf("%w: systemd target is not configured", ErrInvalidInput)
		}
		args := []string{"journalctl", "-u", a.ManagerTarget, "-n", "200", "--no-pager"}
		if a.RunAsRoot {
			cmd = exec.CommandContext(ctx, "sudo", append([]string{"-n"}, args...)...)
		} else {
			cmd = exec.CommandContext(ctx, args[0], args[1:]...)
		}
	case "docker":
		if a.ManagerTarget == "" {
			return LogsResult{}, fmt.Errorf("%w: Docker target is not configured", ErrInvalidInput)
		}
		cmd = exec.CommandContext(ctx, "docker", "logs", "--tail", "200", a.ManagerTarget)
	default:
		return LogsResult{Manager: a.Manager, Target: a.ManagerTarget, Logs: "Brak osobnego źródła logów. Wyjście instalatora i operacji jest dostępne w logach zadań DevBox."}, nil
	}
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return LogsResult{}, fmt.Errorf("read application logs: %w: %s", runErr, strings.TrimSpace(string(out)))
	}
	return LogsResult{Manager: a.Manager, Target: a.ManagerTarget, Logs: string(out)}, nil
}
