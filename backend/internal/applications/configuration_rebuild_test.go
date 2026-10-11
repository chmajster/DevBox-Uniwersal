package applications

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModuleChangeQueuesRebuildButReorderingDoesNot(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "reorder", true: "new-extension"}[changed], func(t *testing.T) {
			f := newFixture(t)
			if err := f.runner.Register(NewDeploymentActionHandler(f.service, f.runner, JobRebuild)); err != nil {
				t.Fatal(err)
			}
			app := f.create(t)
			config := map[string]any{"deployment_mode": "auto", "runtime": "php", "runtime_version": "8.4", "modules": []string{"pdo_mysql", "zip"}}
			if _, err := f.service.Update(context.Background(), app.ID, UpdateInput{Configuration: &config}); err != nil {
				t.Fatal(err)
			}
			_, job := f.deploy(t, app.ID)
			if _, err := f.run(t, job); err != nil {
				t.Fatal(err)
			}
			config["modules"] = []string{"zip", "pdo_mysql"}
			if changed {
				config["modules"] = []string{"zip", "pdo_mysql", "intl"}
			}
			body, _ := json.Marshal(UpdateInput{Configuration: &config})
			request := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(string(body)))
			request.SetPathValue("id", app.ID)
			response := httptest.NewRecorder()
			NewModule(f.service, nil).update(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("update failed: %d %s", response.Code, response.Body.String())
			}
			var count int
			if err := f.db.QueryRow("SELECT COUNT(*) FROM jobs WHERE application_id=? AND type=?", app.ID, JobRebuild).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if (count == 1) != changed {
				t.Fatalf("rebuild count=%d changed=%v", count, changed)
			}
		})
	}
}
