package docker

import (
	"strings"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
)

func TestManagedMountArgsAddsLiveBindAndDependencyVolume(t *testing.T) {
	dir := t.TempDir()
	args, err := managedMountArgs(containerspec.DeploymentSpec{
		BindMounts:       map[string]string{dir: "/app"},
		AnonymousVolumes: []string{"/app/vendor"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "--mount|type=bind,source=" + dir + ",target=/app|--mount|type=volume,target=/app/vendor"
	if got := strings.Join(args, "|"); got != want {
		t.Fatalf("managedMountArgs() = %s, want %s", got, want)
	}
}

func TestManagedMountArgsRejectsRelativeSource(t *testing.T) {
	_, err := managedMountArgs(containerspec.DeploymentSpec{
		BindMounts: map[string]string{"relative/path": "/app"},
	})
	if err == nil {
		t.Fatal("expected relative bind mount source to be rejected")
	}
}
