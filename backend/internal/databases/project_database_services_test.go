package databases

import (
	"context"
	"reflect"
	"testing"
)

func TestNormalizeProjectDatabaseServiceEngines(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    []string
		wantErr bool
	}{
		{name: "mysql", input: []string{"mysql"}, want: []string{"mysql"}},
		{name: "mariadb aliases mysql", input: []string{"mariadb"}, want: []string{"mysql"}},
		{name: "postgres alias", input: []string{"postgres"}, want: []string{"postgresql"}},
		{name: "both deterministic", input: []string{"postgresql", "mysql", "postgres", "mysql"}, want: []string{"mysql", "postgresql"}},
		{name: "empty", input: nil, want: []string{}},
		{name: "unsupported", input: []string{"sqlite"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeProjectDatabaseServiceEngines(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("normalizeProjectDatabaseServiceEngines(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestProjectDatabaseServicesPersistMySQLPostgreSQLAndBoth(t *testing.T) {
	ctx := context.Background()
	service, _, _, _ := databaseBindingTestService(t)

	for _, tt := range []struct {
		name    string
		engines []string
		want    []string
	}{
		{name: "mysql", engines: []string{"mysql"}, want: []string{"mysql"}},
		{name: "postgresql", engines: []string{"postgresql"}, want: []string{"postgresql"}},
		{name: "both", engines: []string{"postgresql", "mysql"}, want: []string{"mysql", "postgresql"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			saved, err := service.UpdateProjectDatabaseServices(ctx, "project-1", ProjectDatabaseServicesInput{Engines: tt.engines}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(saved.Engines, tt.want) {
				t.Fatalf("saved engines = %v, want %v", saved.Engines, tt.want)
			}

			loaded, err := service.GetProjectDatabaseServices(ctx, "project-1")
			if err != nil {
				t.Fatal(err)
			}
			if loaded.ProjectID != "project-1" || !reflect.DeepEqual(loaded.Engines, tt.want) {
				t.Fatalf("unexpected persisted project database services: %+v", loaded)
			}
		})
	}
}

func TestProjectDatabaseServicesCanBeCleared(t *testing.T) {
	ctx := context.Background()
	service, _, _, _ := databaseBindingTestService(t)

	if _, err := service.UpdateProjectDatabaseServices(ctx, "project-1", ProjectDatabaseServicesInput{Engines: []string{"mysql", "postgresql"}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateProjectDatabaseServices(ctx, "project-1", ProjectDatabaseServicesInput{Engines: []string{}}, nil, nil); err != nil {
		t.Fatal(err)
	}

	loaded, err := service.GetProjectDatabaseServices(ctx, "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Engines) != 0 {
		t.Fatalf("expected project database services to be cleared, got %v", loaded.Engines)
	}
}
