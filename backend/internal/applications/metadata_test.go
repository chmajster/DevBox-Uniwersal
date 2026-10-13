package applications

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestMetadataPatchPreservesRunningApplication(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	app := f.create(t)
	_, job := f.deploy(t, app.ID)
	if _, err := f.run(t, job); err != nil {
		t.Fatal(err)
	}
	before, err := f.service.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Detected legacy apps may have no explicit runtime/version. Metadata
	// updates must not require submitting or validating a new runtime plan.
	request := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"name":"  Nowa nazwa  ","description":"  Nowy opis  "}`))
	request.SetPathValue("id", app.ID)
	response := httptest.NewRecorder()
	NewModule(f.service, nil).update(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("metadata patch: %d %s", response.Code, response.Body.String())
	}
	after, err := f.service.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	before.Name = "Nowa nazwa"
	before.Description = "Nowy opis"
	before.UpdatedAt = after.UpdatedAt
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("metadata patch changed application resources:\nbefore=%+v\nafter=%+v", before, after)
	}
	var jobCount int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM jobs WHERE application_id=?", app.ID).Scan(&jobCount); err != nil || jobCount != 1 {
		t.Fatalf("metadata patch enqueued work: count=%d err=%v", jobCount, err)
	}
	list, err := f.service.List(ctx)
	if err != nil || len(list) != 1 || list[0].Name != after.Name {
		t.Fatalf("application list did not reflect the new name: %+v %v", list, err)
	}
}

func TestInvalidMetadataPatchKeepsOriginalApplication(t *testing.T) {
	for _, name := range []string{"   ", " Existing application "} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			app := f.create(t)
			if _, err := f.service.Create(ctx, CreateInput{Name: "Existing application", SourceType: SourceEmpty}, nil); err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]string{"name": name, "description": "Changed description"})
			request := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(string(body)))
			request.SetPathValue("id", app.ID)
			response := httptest.NewRecorder()
			NewModule(f.service, nil).update(response, request)
			want := http.StatusConflict
			if strings.TrimSpace(name) == "" {
				want = http.StatusBadRequest
			}
			if response.Code != want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, want, response.Body.String())
			}
			unchanged, err := f.service.Get(ctx, app.ID)
			if err != nil || !reflect.DeepEqual(app, unchanged) {
				t.Fatalf("rejected metadata patch changed application: %+v err=%v", unchanged, err)
			}
		})
	}
}
