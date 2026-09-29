package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func writeExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFindMySQLServerDetectsMySQL(t *testing.T) {
	dir := t.TempDir()
	path := writeExecutable(t, dir, "mysqld")
	t.Setenv("PATH", dir)

	gotPath, engine, err := findMySQLServer()
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != path || engine != "mysql" {
		t.Fatalf("findMySQLServer() = (%q, %q), want (%q, mysql)", gotPath, engine, path)
	}
}

func TestFindMySQLServerDetectsMariaDB(t *testing.T) {
	dir := t.TempDir()
	path := writeExecutable(t, dir, "mariadbd")
	t.Setenv("PATH", dir)

	gotPath, engine, err := findMySQLServer()
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != path || engine != "mariadb" {
		t.Fatalf("findMySQLServer() = (%q, %q), want (%q, mariadb)", gotPath, engine, path)
	}
}

func TestFindMySQLClientSupportsMariaDBClient(t *testing.T) {
	dir := t.TempDir()
	path := writeExecutable(t, dir, "mariadb")
	t.Setenv("PATH", dir)

	got, err := findMySQLClient()
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("findMySQLClient() = %q, want %q", got, path)
	}
}
