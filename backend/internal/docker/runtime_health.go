package docker

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
)

func (p *CLIProvider) waitManagedReady(parent context.Context, name string, port int, h containerspec.Healthcheck) error {
	target, err := containerspec.HealthcheckURL(h, port)
	if err != nil {
		return err
	}
	startup, timeout := h.StartupSeconds, h.TimeoutSeconds
	if startup == 0 {
		startup = 60
	}
	if timeout == 0 {
		timeout = 2
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(startup)*time.Second)
	defer cancel()
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	u, _ := url.Parse(target)
	last := "application not ready"
	for {
		attempt, stop := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		out, _, inspectErr := p.runner.Run(attempt, "container", "inspect", "--format", "{{.State.Running}}", name)
		if inspectErr == nil && strings.TrimSpace(string(out)) == "true" {
			if u.Scheme == "tcp" {
				conn, e := (&net.Dialer{}).DialContext(attempt, "tcp", u.Host)
				if e == nil {
					_ = conn.Close()
					stop()
					return nil
				}
				last = "TCP connection failed"
			} else {
				req, _ := http.NewRequestWithContext(attempt, http.MethodGet, target, nil)
				response, e := client.Do(req)
				if e == nil {
					_ = response.Body.Close()
					accepted := len(h.ExpectedStatuses) == 0 && response.StatusCode >= 200 && response.StatusCode < 400
					for _, s := range h.ExpectedStatuses {
						if response.StatusCode == s {
							accepted = true
						}
					}
					if accepted {
						stop()
						return nil
					}
					last = fmt.Sprintf("HTTP status %d", response.StatusCode)
				} else {
					last = "HTTP connection failed"
				}
			}
		} else if inspectErr != nil {
			last = "could not inspect container"
		} else {
			last = "container is not running"
		}
		stop()
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("managed container healthcheck %s failed (%s): %w", target, last, ctx.Err())
		case <-timer.C:
		}
	}
}

// Probe as the configured container user. This never changes host permissions.
func (p *CLIProvider) checkManagedAccess(ctx context.Context, spec containerspec.DeploymentSpec) error {
	if spec.Runtime == "go" || spec.Runtime == "custom" {
		return nil
	}
	for _, root := range spec.BindMounts {
		check, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, _, err := p.runner.Run(check, "container", "exec", spec.ContainerName, "sh", "-c", `test -r "$1" && test -x "$1"`, "devbox-access", root)
		cancel()
		if err != nil {
			return fmt.Errorf("runtime user cannot read/traverse source mount %s; set matching non-root UID/GID or grant directory access", root)
		}
	}
	for _, dir := range spec.WritablePaths {
		check, cancel := context.WithTimeout(ctx, 5*time.Second)
		// mktemp verifies an actual write and cleanup, not merely Unix permission bits.
		_, _, err := p.runner.Run(check, "container", "exec", spec.ContainerName, "sh", "-c", `test -d "$1" && f=$(mktemp "$1/.devbox-write-check.XXXXXX") && rm -f "$f"`, "devbox-access", dir)
		cancel()
		if err != nil {
			return fmt.Errorf("runtime user cannot write %s; create the directory and set ownership/ACL for the configured UID/GID", dir)
		}
	}
	return nil
}
