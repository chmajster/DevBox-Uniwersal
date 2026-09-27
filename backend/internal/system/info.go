package system

import (
	"os"
	"runtime"
)

type Info struct {
	Hostname  string `json:"hostname"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	GoVersion string `json:"go_version"`
	Version   string `json:"version"`
}

func Current(version string) Info {
	hostname, _ := os.Hostname()
	return Info{Hostname: hostname, OS: runtime.GOOS, Arch: runtime.GOARCH, GoVersion: runtime.Version(), Version: version}
}
