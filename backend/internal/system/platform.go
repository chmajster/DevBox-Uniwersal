package system

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
)

type PlatformInfo struct {
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	DistroID      string `json:"distro_id,omitempty"`
	DistroName    string `json:"distro_name,omitempty"`
	DistroVersion string `json:"distro_version,omitempty"`
	WSL           bool   `json:"wsl"`
	WSLVersion    int    `json:"wsl_version,omitempty"`
	Systemd       bool   `json:"systemd"`
}

func DetectPlatform() PlatformInfo {
	osRelease, _ := os.ReadFile("/etc/os-release")
	kernelRelease, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	env := map[string]string{
		"WSL_DISTRO_NAME": os.Getenv("WSL_DISTRO_NAME"),
		"WSL_INTEROP":     os.Getenv("WSL_INTEROP"),
	}
	info := DetectPlatformFrom(runtime.GOOS, runtime.GOARCH, string(osRelease), string(kernelRelease), env)
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		info.Systemd = true
	}
	return info
}

func DetectPlatformFrom(goos, arch, osRelease, kernelRelease string, env map[string]string) PlatformInfo {
	fields := ParseOSRelease(osRelease)
	info := PlatformInfo{
		OS:            goos,
		Arch:          arch,
		DistroID:      fields["ID"],
		DistroName:    fields["PRETTY_NAME"],
		DistroVersion: fields["VERSION_ID"],
	}
	kernelLower := strings.ToLower(kernelRelease)
	if env["WSL_DISTRO_NAME"] != "" || env["WSL_INTEROP"] != "" || strings.Contains(kernelLower, "microsoft") || strings.Contains(kernelLower, "wsl") {
		info.WSL = true
		info.WSLVersion = 1
		if env["WSL_INTEROP"] != "" || strings.Contains(kernelLower, "wsl2") || strings.Contains(kernelLower, "microsoft-standard") {
			info.WSLVersion = 2
		}
	}
	return info
}

func ParseOSRelease(raw string) map[string]string {
	result := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		} else {
			value = strings.Trim(value, "'\"")
		}
		result[strings.TrimSpace(key)] = value
	}
	return result
}
