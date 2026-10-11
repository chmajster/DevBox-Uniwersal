package applications_test

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/applications"
)

func TestApplicationDockerEntrypointsAndVersions(t *testing.T) {
	f := newHostingDockerFixture(t)
	for _, language := range []struct {
		name     string
		versions []string
	}{{"python", []string{"3.12", "3.13"}}, {"go", []string{"1.25", "1.26"}}} {
		t.Run(language.name, func(t *testing.T) {
			previousT := f.t
			f.t = t
			defer func() { f.t = previousT }()
			apps := []applications.Detail{}
			for index, version := range language.versions {
				files := map[string]string{}
				command, target := "", ""
				if language.name == "python" {
					files["requirements.txt"] = "fastapi==0.115.6\nuvicorn==0.34.0\n"
					target = "web/api.py"
					files[target] = "from fastapi import FastAPI\nimport sys\nfrom fastapi.responses import PlainTextResponse\napi = FastAPI()\n@api.get('/')\ndef index():\n return PlainTextResponse('first '+sys.version.split()[0])\n"
				} else {
					files["go.mod"] = "module example.test/devbox\ngo 1.23\n"
					target = "cmd/web/server.go"
					files[target] = "package main\nimport (\"net/http\";\"runtime\")\nfunc main(){http.HandleFunc(\"/\",func(w http.ResponseWriter,r *http.Request){w.Write([]byte(\"first \"+runtime.Version()))});http.ListenAndServe(\":8080\",nil)}"
					files["cmd/other/main.go"] = "package main\nfunc main(){}"
					command = "devbox-go-start ./cmd/web"
					if index == 1 {
						command = "devbox-go-watch ./cmd/web"
					}
				}
				source := f.source(fmt.Sprintf("%s-%s", language.name, version), files)
				config := map[string]any{"deployment_mode": "auto", "runtime": language.name, "runtime_version": version, "development_reload": index == 1}
				if command != "" {
					config["start_command"] = command
				}
				app := f.create(applications.CreateInput{Name: language.name + " " + version, SourceType: applications.SourceLocal, Source: applications.SourceInput{LocalPath: source}, Configuration: config})
				app = f.deploy(app.ID)
				body := f.http(app)
				if !strings.Contains(body, version) || !strings.Contains(body, "first") {
					t.Fatalf("HTTP/runtime version: %s", body)
				}
				container := app.Workloads[0].DriverResourceID
				if out, err := exec.CommandContext(f.ctx, "docker", "exec", container, "sh", "-c", "printf rw > /app/container-write.txt").CombinedOutput(); err != nil {
					t.Fatalf("RW write: %s %v", out, err)
				}
				if data, err := os.ReadFile(filepath.Join(source, "container-write.txt")); err != nil || string(data) != "rw" {
					t.Fatalf("container write missing on host: %s %v", data, err)
				}
				updated := strings.Replace(files[target], "first", "second", 1)
				if err := os.WriteFile(filepath.Join(source, target), []byte(updated), 0o644); err != nil {
					t.Fatal(err)
				}
				if out, err := exec.CommandContext(f.ctx, "docker", "exec", container, "cat", "/app/"+target).Output(); err != nil || !strings.Contains(string(out), "second") {
					t.Fatalf("host edit invisible: %s %v", out, err)
				}
				if index == 0 {
					app = f.rebuild(app.ID)
				} else {
					deadline := time.Now().Add(90 * time.Second)
					client := &http.Client{Timeout: 2 * time.Second}
					for time.Now().Before(deadline) {
						response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", *app.Endpoints[0].HostPort))
						if err == nil {
							data, _ := io.ReadAll(response.Body)
							response.Body.Close()
							if response.StatusCode == 200 && strings.Contains(string(data), "second") {
								break
							}
						}
						time.Sleep(time.Second)
					}
				}
				if !strings.Contains(f.http(app), "second") {
					t.Fatal("rebuild/reload did not serve new code")
				}
				apps = append(apps, app)
			}
			// Both independent versions must remain running at the same time.
			for index, app := range apps {
				if !strings.Contains(f.http(app), language.versions[index]) {
					t.Fatal("runtime replacement affected another application")
				}
			}
		})
	}
}
