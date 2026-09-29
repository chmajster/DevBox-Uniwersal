package docker

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
)

// Exercises the real generator, image build, mount layout, readiness and rollback.
// Isolated Docker runner only: it creates and removes its own labelled resources.
func TestAuditManagedRuntimeDockerE2E(t *testing.T) {
	if os.Getenv("DEVBOX_AUDIT_DOCKER_E2E") != "1" {
		t.Skip("set DEVBOX_AUDIT_DOCKER_E2E=1 on an isolated Docker runner")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	p := NewCLIProvider()
	if err := p.Available(ctx); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	project := fmt.Sprintf("audit-%d", time.Now().UnixNano())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	files := map[string]string{
		"package.json": `{"name":"audit-runtime","version":"1.0.0","private":true,"scripts":{"start":"node dist/server.js"}}`,
		"build.cjs":    `const fs=require('node:fs');fs.mkdirSync('dist',{recursive:true});const value=fs.readFileSync('message.txt','utf8');fs.writeFileSync('dist/server.js',"const http=require('node:http');http.createServer((req,res)=>{if(req.url==='/ready'){res.writeHead(204);return res.end()}if(req.url==='/value'){return res.end("+JSON.stringify(value)+")}res.writeHead(500);res.end('not ready path')}).listen(Number(process.env.PORT),'0.0.0.0');");`,
		"message.txt":  "first-release",
	}
	for name, body := range files {
		if err = os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	images := []string{}
	container := ""
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 60*time.Second)
		defer stop()
		if container != "" {
			_, _, _ = p.runner.Run(cleanup, "container", "rm", "-f", "-v", container)
		}
		for _, image := range images {
			_, _, _ = p.runner.Run(cleanup, "image", "rm", image)
		}
	})
	makeSpec := func(mode, start string) containerspec.DeploymentSpec {
		t.Helper()
		spec, err := containerspec.GenerateManaged(project, dir, "node", "22", nil, "fixture", port)
		if err != nil {
			t.Fatal(err)
		}
		spec, err = containerspec.WithExecution(spec, containerspec.ExecutionOptions{SourceMode: mode, Healthcheck: containerspec.Healthcheck{Target: "/ready", ExpectedStatuses: []int{204}, StartupSeconds: 3}}, "node build.cjs", start, "")
		if err != nil {
			t.Fatal(err)
		}
		images = append(images, spec.Image)
		container = spec.ContainerName
		if err = p.BuildManaged(ctx, spec); err != nil {
			t.Fatal(err)
		}
		return spec
	}
	request := func() string {
		t.Helper()
		client := http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/value")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("application unavailable: %d %v", resp.StatusCode, err)
		}
		return string(body)
	}
	live := makeSpec("live", "exec node dist/server.js")
	if _, err = os.Stat(filepath.Join(dir, "dist")); !os.IsNotExist(err) {
		t.Fatal("fixture unexpectedly has host build outputs")
	}
	if err = p.ReplaceManagedPorts(ctx, live, nil); err != nil {
		t.Fatal(err)
	}
	if got := request(); got != "first-release" {
		t.Fatal(got)
	}
	versioned := makeSpec("versioned", "exec node dist/server.js")
	if err = p.ReplaceManagedPorts(ctx, versioned, nil); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "message.txt"), []byte("modified-source"), 0644); err != nil {
		t.Fatal(err)
	}
	broken := makeSpec("versioned", "exit 7")
	if err = p.ReplaceManagedPorts(ctx, broken, nil); err == nil {
		t.Fatal("broken new image accepted")
	}
	if got := request(); got != "first-release" {
		t.Fatalf("rollback did not restore code: %s", got)
	}
	detail, err := p.InspectContainer(ctx, container)
	if err != nil || !detail.Running || detail.Image != versioned.Image {
		t.Fatalf("incorrect rollback runtime: %#v %v", detail, err)
	}
	if strings.Contains(detail.Image, broken.Image) {
		t.Fatal("broken image remains active")
	}
}
