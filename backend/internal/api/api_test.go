package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/auth"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
)

func TestLoginMeAndHealth(t *testing.T) {
	tmp := t.TempDir()
	db, err := database.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migrations := filepath.Join("..", "..", "..", "migrations")
	if _, err := os.Stat(migrations); err != nil {
		t.Fatalf("migrations path unavailable: %v", err)
	}
	if err := database.Migrate(context.Background(), db, migrations); err != nil {
		t.Fatal(err)
	}
	users := repository.NewSQLiteUsers(db)
	sessions := repository.NewSQLiteSessions(db)
	authSvc := auth.NewService(users, sessions, time.Hour)
	if err := authSvc.BootstrapAdmin(context.Background(), "admin", "foundation-test-password"); err != nil {
		t.Fatal(err)
	}
	h := New(Dependencies{DB: db, Auth: authSvc, Audit: audit.NewService(repository.NewSQLiteAudit(db)), Jobs: repository.NewSQLiteJobs(db), Version: "test"})

	health := httptest.NewRecorder()
	h.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", health.Code, health.Body.String())
	}

	login := httptest.NewRecorder()
	h.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"foundation-test-password"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	var sessionCookie, csrfCookie *http.Cookie
	for _, cookie := range cookies {
		switch cookie.Name {
		case sessionCookieName:
			sessionCookie = cookie
		case csrfCookieName:
			csrfCookie = cookie
		}
	}
	if sessionCookie == nil || csrfCookie == nil {
		t.Fatalf("expected session and CSRF cookies, got %#v", cookies)
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	meReq.AddCookie(sessionCookie)
	me := httptest.NewRecorder()
	h.ServeHTTP(me, meReq)
	if me.Code != http.StatusOK {
		t.Fatalf("me status=%d body=%s", me.Code, me.Body.String())
	}
	if !strings.Contains(me.Body.String(), `"role":"admin"`) {
		t.Fatalf("me response missing role: %s", me.Body.String())
	}

	logoutWithoutCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutWithoutCSRF.AddCookie(sessionCookie)
	blocked := httptest.NewRecorder()
	h.ServeHTTP(blocked, logoutWithoutCSRF)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("logout without CSRF status=%d body=%s", blocked.Code, blocked.Body.String())
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutReq.AddCookie(sessionCookie)
	logoutReq.AddCookie(csrfCookie)
	logoutReq.Header.Set("X-CSRF-Token", csrfCookie.Value)
	logout := httptest.NewRecorder()
	h.ServeHTTP(logout, logoutReq)
	if logout.Code != http.StatusOK {
		t.Fatalf("logout status=%d body=%s", logout.Code, logout.Body.String())
	}
}
