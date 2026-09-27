package api

import (
	"context"
	"crypto/subtle"
	"net/http"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type userContextKey struct{}

func (a *API) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required", nil)
			return
		}
		user, err := a.auth.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required", nil)
			return
		}
		if requiresCSRF(r.Method) {
			csrfCookie, csrfErr := r.Cookie(csrfCookieName)
			csrfHeader := r.Header.Get("X-CSRF-Token")
			if csrfErr != nil || csrfHeader == "" || subtle.ConstantTimeCompare([]byte(csrfCookie.Value), []byte(csrfHeader)) != 1 {
				writeError(w, http.StatusForbidden, "csrf_invalid", "CSRF token is missing or invalid", nil)
				return
			}
		}
		ctx := context.WithValue(r.Context(), userContextKey{}, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requiresCSRF(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func requireRole(min domain.Role, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := currentUser(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required", nil)
			return
		}
		if user.Role.Rank() < min.Rank() {
			writeError(w, http.StatusForbidden, "forbidden", "insufficient permissions", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func currentUser(ctx context.Context) (domain.User, bool) {
	u, ok := ctx.Value(userContextKey{}).(domain.User)
	return u, ok
}

func CurrentUser(ctx context.Context) (domain.User, bool) {
	return currentUser(ctx)
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
