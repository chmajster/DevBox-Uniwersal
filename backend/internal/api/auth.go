package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	authsvc "github.com/chmajster/DevBox-Uniwersal/backend/internal/auth"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON request", nil)
		return
	}
	user, token, session, err := a.auth.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, authsvc.ErrInvalidCredentials) || errors.Is(err, authsvc.ErrInactiveUser) {
			_ = a.audit.Record(r.Context(), nil, "auth.login_failed", "auth", nil, map[string]any{"username": req.Username}, remoteIP(r))
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password", nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "login failed", nil)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: token, Path: "/", HttpOnly: true, Secure: a.cookieSecure, SameSite: http.SameSiteLaxMode, Expires: session.ExpiresAt, MaxAge: int(time.Until(session.ExpiresAt).Seconds())})
	uid := user.ID
	_ = a.audit.Record(r.Context(), &uid, "auth.login", "user", &uid, nil, remoteIP(r))
	writeJSON(w, http.StatusOK, user)
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r.Context())
	cookie, _ := r.Cookie(sessionCookieName)
	if cookie != nil {
		_ = a.auth.Logout(r.Context(), cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true, Secure: a.cookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	uid := user.ID
	_ = a.audit.Record(r.Context(), &uid, "auth.logout", "user", &uid, nil, remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r.Context())
	writeJSON(w, http.StatusOK, user)
}
