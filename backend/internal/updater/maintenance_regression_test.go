package updater

import (
	"bytes"
	"context"
	"crypto/rand"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateSnapshotAuthenticatedRoundTrip(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "state")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "secret")
	original := []byte("do-not-persist-in-plaintext")
	if err := os.WriteFile(file, original, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "binary-link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(root, "new-wal")
	snapshot := filepath.Join(root, "snapshot.enc")
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if err := createUpdateSnapshot(snapshot, []string{dir, link, missing}, key); err != nil {
		t.Fatal(err)
	}
	ciphertext, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, original) {
		t.Fatal("snapshot contains plaintext secret")
	}
	if info, _ := os.Stat(snapshot); info.Mode().Perm() != 0600 {
		t.Fatal("unsafe snapshot permissions")
	}
	os.WriteFile(file, []byte("changed"), 0644)
	os.WriteFile(missing, []byte("new WAL"), 0600)
	changed := append([]byte{}, ciphertext...)
	changed[len(changed)-1] ^= 1
	os.WriteFile(snapshot, changed, 0600)
	if err := restoreUpdateSnapshot(snapshot, key); err == nil {
		t.Fatal("tampered snapshot accepted")
	}
	if data, _ := os.ReadFile(file); string(data) != "changed" {
		t.Fatal("authentication failure modified installation")
	}
	os.WriteFile(snapshot, ciphertext, 0600)
	if err := restoreUpdateSnapshot(snapshot, key); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(file); !bytes.Equal(data, original) {
		t.Fatalf("incorrect restored content: %q", data)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("new WAL was not removed")
	}
	if value, err := os.Readlink(link); err != nil || value != file {
		t.Fatalf("lost symlink: %q %v", value, err)
	}
	if info, _ := os.Stat(file); info.Mode().Perm() != 0600 {
		t.Fatal("lost secret permissions")
	}
}
func TestSnapshotRejectsBroadRootsAndExternalSymlink(t *testing.T) {
	for _, roots := range [][]string{{"/"}, {"/etc"}, {"relative"}, {"/tmp/a", "/tmp/a/b"}} {
		if _, err := safeSnapshotRoots(roots); err == nil {
			t.Fatalf("accepted unsafe roots %#v", roots)
		}
	}
	root := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink("/etc/passwd", link); err != nil {
		t.Fatal(err)
	}
	if err := createUpdateSnapshot(filepath.Join(root, "state.enc"), []string{link}, make([]byte, 32)); err == nil {
		t.Fatal("captured symlink outside roots")
	}
}
func TestUpdateHealthRequiresExactVersionAndSQLite(t *testing.T) {
	for _, tc := range []struct {
		version, db string
		valid       bool
	}{{"new", "ok", true}, {"old", "ok", false}, {"new", "error", false}} {
		t.Run(tc.version+tc.db, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/health" {
					t.Error("wrong health route")
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"data":{"status":"ok","database":"` + tc.db + `","version":"` + tc.version + `"}}`))
			}))
			defer srv.Close()
			err := verifyUpdateHealth(strings.TrimPrefix(srv.URL, "http://"), "new", 1)
			if (err == nil) != tc.valid {
				t.Fatalf("health outcome %v", err)
			}
		})
	}
	if err := verifyUpdateHealth("example.org:80", "new", 1); err == nil {
		t.Fatal("remote updater health target accepted")
	}
}
func TestUpdateRefusesNonIdleJobQueue(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "db.sqlite")
	db, err := database.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = database.Migrate(context.Background(), db, "../../../migrations"); err != nil {
		t.Fatal(err)
	}
	if err = checkUpdateIdle(filename); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO jobs(id,type,status,resource_key,payload_json,created_at) VALUES('busy','test','queued','global:test','{}','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err = checkUpdateIdle(filename); err == nil {
		t.Fatal("update would interrupt queued job")
	}
}
