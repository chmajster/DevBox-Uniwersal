package databases

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

type fakeSecretStore struct {
	values map[string][]byte
}

func (f *fakeSecretStore) Put(_ context.Context, scope, name string, plaintext []byte) error {
	if f.values == nil {
		f.values = make(map[string][]byte)
	}
	f.values[scope+"/"+name] = append([]byte(nil), plaintext...)
	return nil
}

func (f *fakeSecretStore) Get(_ context.Context, scope, name string) ([]byte, error) {
	value, ok := f.values[scope+"/"+name]
	if !ok {
		return nil, secrets.ErrNotFound
	}
	return append([]byte(nil), value...), nil
}

func (f *fakeSecretStore) Delete(_ context.Context, scope, name string) error {
	delete(f.values, scope+"/"+name)
	return nil
}

type fakeMySQLExecutor struct {
	statements []string
	query      string
	err        error
}

func (f *fakeMySQLExecutor) ExecSQL(_ context.Context, statement string) error {
	f.statements = append(f.statements, statement)
	return f.err
}

func (f *fakeMySQLExecutor) QuerySQL(_ context.Context, statement string) (string, error) {
	f.statements = append(f.statements, statement)
	return f.query, f.err
}

func (f *fakeMySQLExecutor) Dump(_ context.Context, _ string, _ io.Writer) error {
	return f.err
}

func (f *fakeMySQLExecutor) Restore(_ context.Context, _ io.Reader) error {
	return f.err
}

func TestValidateIdentifier(t *testing.T) {
	valid := []string{"app", "app_db", "DB123"}
	for _, value := range valid {
		if err := ValidateIdentifier(value); err != nil {
			t.Fatalf("ValidateIdentifier(%q) unexpected error: %v", value, err)
		}
	}
	invalid := []string{"", "db-name", "db name", "db;DROP", "db.name", strings.Repeat("a", 65)}
	for _, value := range invalid {
		if err := ValidateIdentifier(value); err == nil {
			t.Fatalf("ValidateIdentifier(%q) expected error", value)
		}
	}
}

func TestGrantIsScopedToDatabase(t *testing.T) {
	executor := &fakeMySQLExecutor{}
	provider := &MySQLProvider{
		cfg:  MySQLConfig{ApplicationHost: "%"},
		exec: executor,
	}
	if err := provider.Grant(context.Background(), "app_db", "app_user", []string{"SELECT", "INSERT"}); err != nil {
		t.Fatal(err)
	}
	if len(executor.statements) != 1 {
		t.Fatalf("expected one statement, got %d", len(executor.statements))
	}
	statement := executor.statements[0]
	if !strings.Contains(statement, "ON `app_db`.*") {
		t.Fatalf("grant is not database-scoped: %s", statement)
	}
	if strings.Contains(statement, "ON *.*") {
		t.Fatalf("global grant detected: %s", statement)
	}
}

func TestDatabaseUserSecretIsMaskedFromJSON(t *testing.T) {
	payload, err := json.Marshal(DatabaseUser{ID: "u1", Username: "app", SecretRef: "secret-ref"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "secret-ref") || strings.Contains(string(payload), "secret") {
		t.Fatalf("secret reference leaked in JSON: %s", payload)
	}
}

func TestCreateUserErrorDoesNotLeakPassword(t *testing.T) {
	store := &fakeSecretStore{values: map[string][]byte{"database-user/ref": []byte("very-secret-password")}}
	executor := &fakeMySQLExecutor{err: errors.New("mysql execution failed")}
	provider := &MySQLProvider{
		cfg:     MySQLConfig{ApplicationHost: "%"},
		secrets: store,
		exec:    executor,
	}
	err := provider.CreateUser(context.Background(), "app_user", "ref")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "very-secret-password") {
		t.Fatalf("password leaked in error: %v", err)
	}
}

func TestSanitizeMySQLErrorRedactsPasswordAndPreservesDiagnostic(t *testing.T) {
	raw := "ERROR 1045 (28000): Access denied for user 'devbox_admin'@'localhost' password=super-secret"
	got := sanitizeMySQLError(raw)
	if strings.Contains(got, "super-secret") {
		t.Fatalf("password leaked in MySQL error: %s", got)
	}
	if !strings.Contains(got, "ERROR 1045") || !strings.Contains(got, "Access denied") {
		t.Fatalf("diagnostic details were lost: %s", got)
	}
}

func TestSanitizeMySQLErrorTruncatesLongOutput(t *testing.T) {
	got := sanitizeMySQLError(strings.Repeat("x", 800))
	if len(got) > 604 {
		t.Fatalf("expected bounded diagnostic, got %d bytes", len(got))
	}
}
