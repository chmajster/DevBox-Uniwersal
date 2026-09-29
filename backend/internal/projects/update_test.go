package projects

import (
	"context"
	"errors"
	"testing"
)

func TestUpdateAllowsLocalPathAndRuntimeChange(t *testing.T) {
	repo, project, _ := integrationProject(t, Project{
		Runtime:         "php",
		RuntimeVersion:  "8.3",
		ContainerPolicy: ContainerPolicyAuto,
	})
	ctx := context.Background()

	if err := repo.SaveRuntimeContainerConfig(ctx, project.ID, RuntimeContainerConfig{
		ProjectID:       project.ID,
		Runtime:         "php",
		RuntimeVersion:  "8.3",
		ContainerPolicy: ContainerPolicyAuto,
		Modules:         []RuntimeModule{{Name: "mysqli"}},
	}); err != nil {
		t.Fatal(err)
	}

	newPath := t.TempDir()
	newRuntime := "python"
	service := NewService(repo, nil, nil, nil, t.TempDir())

	updated, err := service.Update(ctx, project.ID, UpdateInput{
		LocalPath: &newPath,
		Runtime:   &newRuntime,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	resolvedPath, err := ValidateExistingDirectory(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LocalPath != resolvedPath {
		t.Fatalf("local path = %q, want %q", updated.LocalPath, resolvedPath)
	}
	if updated.Runtime != "python" {
		t.Fatalf("runtime = %q, want python", updated.Runtime)
	}
	if updated.RuntimeVersion != "" {
		t.Fatalf("runtime version = %q, want empty after technology change", updated.RuntimeVersion)
	}

	config, err := repo.RuntimeContainerConfig(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Modules) != 0 {
		t.Fatalf("stale runtime modules were not removed: %+v", config.Modules)
	}
}

func TestUpdateRejectsMissingLocalPath(t *testing.T) {
	repo, project, _ := integrationProject(t, Project{
		Runtime:         "static",
		ContainerPolicy: ContainerPolicyAuto,
	})
	ctx := context.Background()
	missing := project.LocalPath + "-missing"
	service := NewService(repo, nil, nil, nil, t.TempDir())

	_, err := service.Update(ctx, project.ID, UpdateInput{LocalPath: &missing})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Update() error = %v, want ErrInvalidInput", err)
	}
}

func TestUpdateRejectsLocalPathChangeForManagedSource(t *testing.T) {
	repo, project, _ := integrationProject(t, Project{
		Runtime:         "static",
		ContainerPolicy: ContainerPolicyAuto,
	})
	ctx := context.Background()
	if _, err := repo.db.ExecContext(ctx, `UPDATE projects SET source_type='git' WHERE id=?`, project.ID); err != nil {
		t.Fatal(err)
	}

	newPath := t.TempDir()
	service := NewService(repo, nil, nil, nil, t.TempDir())
	_, err := service.Update(ctx, project.ID, UpdateInput{LocalPath: &newPath})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Update() error = %v, want ErrInvalidInput", err)
	}
}
