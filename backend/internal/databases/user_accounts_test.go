package databases

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	controldb "github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
)

func TestDatabaseUserAccountCanHaveMultipleDatabaseGrants(t *testing.T) {
	db, err := controldb.Open(filepath.Join(t.TempDir(), "devbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	migrations := filepath.Join("..", "..", "..", "migrations")
	if _, err := os.Stat(migrations); err != nil {
		t.Fatalf("migrations path unavailable: %v", err)
	}
	if err := controldb.Migrate(context.Background(), db, migrations); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	repo := NewRepository(db)
	now := time.Now().UTC()
	first := Database{ID: "db-1", Provider: "local-mysql", Engine: "mysql", Name: "app_one", Status: "ready", CreatedAt: now, UpdatedAt: now}
	second := Database{ID: "db-2", Provider: "local-mysql", Engine: "mysql", Name: "app_two", Status: "ready", CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateDatabase(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateDatabase(ctx, second); err != nil {
		t.Fatal(err)
	}

	user := DatabaseUser{
		ID: "user-1", Engine: "mysql", DatabaseID: first.ID, Username: "app_user",
		SecretRef: "secret-1", Privileges: []string{"SELECT"}, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertUserDatabaseGrant(ctx, user.ID, second.ID, []string{"SELECT", "INSERT", "UPDATE"}); err != nil {
		t.Fatal(err)
	}

	got, err := repo.UserByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Engine != "mysql" || got.Username != "app_user" {
		t.Fatalf("unexpected account: %+v", got)
	}
	if len(got.Databases) != 2 {
		t.Fatalf("expected two database grants, got %+v", got.Databases)
	}

	users, err := repo.UsersByDatabase(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].ID != user.ID {
		t.Fatalf("database assignment not discoverable: %+v", users)
	}
	var secondGrant *DatabaseUserGrant
	for i := range got.Databases {
		if got.Databases[i].DatabaseID == second.ID {
			secondGrant = &got.Databases[i]
			break
		}
	}
	if secondGrant == nil || len(secondGrant.Privileges) != 3 {
		t.Fatalf("unexpected second database privileges: %+v", secondGrant)
	}

	if err := repo.DeleteUserDatabaseGrant(ctx, user.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	got, err = repo.UserByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Databases) != 1 || got.Databases[0].DatabaseID != second.ID {
		t.Fatalf("removing one database must preserve the account and other grants: %+v", got)
	}
}
