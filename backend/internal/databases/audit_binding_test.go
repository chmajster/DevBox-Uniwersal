package databases

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBindingSelectsExplicitUserAndPreventsDeletion(t *testing.T) {
	ctx := context.Background()
	service, repo, store, _ := databaseBindingTestService(t)
	project := "project-1"
	now := time.Now().UTC()
	db := Database{ID: "audit-db", ProjectID: &project, Provider: "local-mysql", Engine: "mysql", Name: "audit_database", Status: "ready", CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"older", "selected"} {
		user := DatabaseUser{ID: id, DatabaseID: db.ID, Username: "audit_" + id, SecretRef: id, Privileges: defaultPrivileges(), CreatedAt: now, UpdatedAt: now}
		if err := repo.CreateUser(ctx, user); err != nil {
			t.Fatal(err)
		}
		if err := store.Put(ctx, "database-user", id, []byte(id+"-password")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.UpdateDatabaseBinding(ctx, project, DatabaseBindingInput{Mode: DatabaseModeManaged}, nil, nil); err == nil {
		t.Fatal("ambiguous account selected implicitly")
	}
	binding, err := service.UpdateDatabaseBinding(ctx, project, DatabaseBindingInput{Mode: DatabaseModeManaged, DatabaseUserID: "selected"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if binding.DatabaseUserID == nil || *binding.DatabaseUserID != "selected" {
		t.Fatal("selected account not persisted")
	}
	connection, err := service.ResolveApplicationConnection(ctx, project)
	if err != nil {
		t.Fatal(err)
	}
	if connection.Username != "audit_selected" || connection.SecretRef != "selected" {
		t.Fatal("resolver used first account instead of selected account")
	}
	if err = service.DeleteUser(ctx, "selected", nil, nil); !errors.Is(err, ErrDatabaseUserInUse) {
		t.Fatalf("did not reject in-use account before contacting MySQL: %v", err)
	}
	if _, err = repo.UserByID(ctx, "selected"); err != nil {
		t.Fatal("deleted bound account")
	}
	if _, err = service.UpdateDatabaseBinding(ctx, project, DatabaseBindingInput{Mode: DatabaseModeManaged, DatabaseUserID: "missing"}, nil, nil); err == nil {
		t.Fatal("accepted nonexistent selected user")
	}
}
