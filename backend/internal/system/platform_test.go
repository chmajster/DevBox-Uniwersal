package system

import "testing"

func TestDetectWSL2(t *testing.T) {
	info := DetectPlatformFrom("linux", "amd64", "ID=ubuntu\nVERSION_ID=24.04\nPRETTY_NAME=Ubuntu 24.04 LTS\n", "6.6.87.2-microsoft-standard-WSL2", map[string]string{"WSL_DISTRO_NAME": "Ubuntu", "WSL_INTEROP": "/run/WSL/1_interop"})
	if !info.WSL || info.WSLVersion != 2 {
		t.Fatalf("expected WSL2, got %+v", info)
	}
	if info.DistroID != "ubuntu" || info.DistroVersion != "24.04" {
		t.Fatalf("unexpected distro: %+v", info)
	}
}

func TestDetectNativeLinux(t *testing.T) {
	info := DetectPlatformFrom("linux", "arm64", "ID=debian\n", "6.1.0-amd64", map[string]string{})
	if info.WSL || info.WSLVersion != 0 {
		t.Fatalf("expected native linux, got %+v", info)
	}
}
