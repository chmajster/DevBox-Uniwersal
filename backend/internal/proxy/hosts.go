package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type HostsManager interface {
	Ensure(context.Context, string, string) (HostChange, error)
	Remove(context.Context, string) (HostChange, error)
}

type FileHostsManager struct {
	path string
}

func NewFileHostsManager(path string) *FileHostsManager {
	return &FileHostsManager{path: path}
}

func DefaultHostsPath(configured string) string {
	if strings.TrimSpace(configured) != "" {
		return configured
	}
	if runtime.GOOS == "windows" {
		root := os.Getenv("WINDIR")
		if root == "" {
			root = `C:\Windows`
		}
		return filepath.Join(root, "System32", "drivers", "etc", "hosts")
	}
	if os.Getenv("WSL_INTEROP") != "" || os.Getenv("WSL_DISTRO_NAME") != "" {
		windowsHosts := "/mnt/c/Windows/System32/drivers/etc/hosts"
		if _, err := os.Stat(windowsHosts); err == nil {
			return windowsHosts
		}
	}
	return "/etc/hosts"
}

func (h *FileHostsManager) Ensure(ctx context.Context, hostname, address string) (HostChange, error) {
	if err := ctx.Err(); err != nil {
		return HostChange{}, err
	}
	hostname, err := NormalizeHostname(hostname)
	if err != nil {
		return HostChange{}, err
	}
	if net.ParseIP(address) == nil {
		return HostChange{}, fmt.Errorf("%w: hosts address is not a valid IP", ErrInvalidInput)
	}
	data, mode, err := h.read()
	if err != nil {
		return HostChange{}, err
	}
	lines := splitLines(string(data))
	for _, line := range lines {
		fields := strings.Fields(strings.SplitN(line, "#", 2)[0])
		if len(fields) < 2 {
			continue
		}
		for _, name := range fields[1:] {
			if strings.EqualFold(name, hostname) {
				if fields[0] == address {
					return HostChange{Applied: true, Path: h.path}, nil
				}
				return HostChange{
					Path: h.path,
					Instruction: "Hostname " + hostname + " already exists in " + h.path +
						" with address " + fields[0] + "; update the conflicting entry manually.",
				}, nil
			}
		}
	}

	content := strings.TrimRight(string(data), "\r\n")
	if content != "" {
		content += "\n"
	}
	content += address + "\t" + hostname + "\t# devbox\n"
	if err := atomicWriteFile(h.path, []byte(content), mode); err != nil {
		if os.IsPermission(err) {
			return HostChange{
				Path:              h.path,
				RequiresPrivilege: true,
				Instruction: "Add the following line to " + h.path +
					" with Administrator/root privileges: " + address + " " + hostname,
			}, nil
		}
		return HostChange{}, fmt.Errorf("write hosts file: %w", err)
	}
	return HostChange{Applied: true, Path: h.path}, nil
}

func (h *FileHostsManager) Remove(ctx context.Context, hostname string) (HostChange, error) {
	if err := ctx.Err(); err != nil {
		return HostChange{}, err
	}
	hostname, err := NormalizeHostname(hostname)
	if err != nil {
		return HostChange{}, err
	}
	data, mode, err := h.read()
	if err != nil {
		return HostChange{}, err
	}
	lines := splitLines(string(data))
	filtered := make([]string, 0, len(lines))
	removed := false
	for _, line := range lines {
		if isManagedHostLine(line, hostname) {
			removed = true
			continue
		}
		filtered = append(filtered, line)
	}
	if !removed {
		return HostChange{Applied: true, Path: h.path}, nil
	}
	content := strings.Join(filtered, "\n")
	content = strings.TrimRight(content, "\n")
	if content != "" {
		content += "\n"
	}
	if err := atomicWriteFile(h.path, []byte(content), mode); err != nil {
		if os.IsPermission(err) {
			return HostChange{
				Path:              h.path,
				RequiresPrivilege: true,
				Instruction: "Remove the DevBox-managed entry for " + hostname +
					" from " + h.path + " with Administrator/root privileges.",
			}, nil
		}
		return HostChange{}, fmt.Errorf("write hosts file: %w", err)
	}
	return HostChange{Applied: true, Path: h.path}, nil
}

func (h *FileHostsManager) read() ([]byte, os.FileMode, error) {
	info, err := os.Stat(h.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, fmt.Errorf("hosts file %s does not exist", h.path)
		}
		return nil, 0, fmt.Errorf("stat hosts file: %w", err)
	}
	data, err := os.ReadFile(h.path)
	if err != nil {
		return nil, 0, fmt.Errorf("read hosts file: %w", err)
	}
	return data, info.Mode().Perm(), nil
}

func splitLines(value string) []string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.Split(value, "\n")
}

func isManagedHostLine(line, hostname string) bool {
	if !strings.Contains(line, "# devbox") {
		return false
	}
	fields := strings.Fields(strings.SplitN(line, "#", 2)[0])
	if len(fields) < 2 {
		return false
	}
	for _, name := range fields[1:] {
		if strings.EqualFold(name, hostname) {
			return true
		}
	}
	return false
}
