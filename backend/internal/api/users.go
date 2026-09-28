package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	authsvc "github.com/chmajster/DevBox-Uniwersal/backend/internal/auth"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
)

type createUserRequest struct {
	Username string      `json:"username"`
	Password *string     `json:"password"`
	Generate bool        `json:"generate_password"`
	Role     domain.Role `json:"role"`
	Active   *bool       `json:"active"`
}

type updateUserRequest struct {
	Role   domain.Role `json:"role"`
	Active *bool       `json:"active"`
}

type changeUserPasswordRequest struct {
	Password *string `json:"password"`
	Generate bool    `json:"generate"`
}

func (a *API) listUsers(w http.ResponseWriter, r *http.Request) {
	items, err := a.auth.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list users", nil)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) createUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeUserJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	password := req.Password
	if req.Generate {
		password = nil
	} else if password == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "password is required unless generate_password is true", nil)
		return
	}
	user, plainPassword, err := a.auth.CreateUser(r.Context(), req.Username, password, req.Role, active)
	if err != nil {
		a.writeUserManagementError(w, err)
		return
	}
	actor, _ := currentUser(r.Context())
	actorID := actor.ID
	_ = a.audit.Record(r.Context(), &actorID, "user.create", "user", &user.ID, map[string]any{
		"username": user.Username,
		"role":     user.Role,
		"active":   user.Active,
	}, remoteIP(r))
	writeJSON(w, http.StatusCreated, map[string]any{"user": user, "password": plainPassword})
}

func (a *API) updateUser(w http.ResponseWriter, r *http.Request) {
	var req updateUserRequest
	if err := decodeUserJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if req.Active == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "active is required", nil)
		return
	}
	id := r.PathValue("id")
	actor, _ := currentUser(r.Context())
	if actor.ID == id && (req.Role != domain.RoleAdmin || !*req.Active) {
		writeError(w, http.StatusBadRequest, "self_protection", "current admin account cannot be demoted or disabled", nil)
		return
	}
	user, err := a.auth.UpdateUserAccess(r.Context(), id, req.Role, *req.Active)
	if err != nil {
		a.writeUserManagementError(w, err)
		return
	}
	actorID := actor.ID
	_ = a.audit.Record(r.Context(), &actorID, "user.update", "user", &id, map[string]any{
		"role": user.Role, "active": user.Active,
	}, remoteIP(r))
	writeJSON(w, http.StatusOK, user)
}

func (a *API) changeUserPassword(w http.ResponseWriter, r *http.Request) {
	var req changeUserPasswordRequest
	if err := decodeUserJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	password := req.Password
	if req.Generate {
		password = nil
	} else if password == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "password is required unless generate is true", nil)
		return
	}
	id := r.PathValue("id")
	plainPassword, err := a.auth.ChangeUserPassword(r.Context(), id, password)
	if err != nil {
		a.writeUserManagementError(w, err)
		return
	}
	actor, _ := currentUser(r.Context())
	actorID := actor.ID
	_ = a.audit.Record(r.Context(), &actorID, "user.password.change", "user", &id, map[string]any{
		"sessions_revoked": true,
		"generated":        req.Generate,
	}, remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]any{"status": "password_changed", "password": plainPassword, "sessions_revoked": true})
}

func (a *API) revokeUserSessions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.auth.RevokeUserSessions(r.Context(), id); err != nil {
		a.writeUserManagementError(w, err)
		return
	}
	actor, _ := currentUser(r.Context())
	actorID := actor.ID
	_ = a.audit.Record(r.Context(), &actorID, "user.sessions.revoke", "user", &id, nil, remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "sessions_revoked"})
}

func (a *API) deleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	actor, _ := currentUser(r.Context())
	if actor.ID == id {
		writeError(w, http.StatusBadRequest, "self_protection", "current admin account cannot be deleted", nil)
		return
	}
	if err := a.auth.DeleteUser(r.Context(), id); err != nil {
		a.writeUserManagementError(w, err)
		return
	}
	actorID := actor.ID
	_ = a.audit.Record(r.Context(), &actorID, "user.delete", "user", &id, nil, remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func decodeUserJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return errors.New("invalid JSON request")
	}
	return nil
}

func (a *API) writeUserManagementError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "user not found", nil)
	case errors.Is(err, authsvc.ErrInvalidUserInput):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
	case errors.Is(err, authsvc.ErrUsernameExists), strings.Contains(strings.ToLower(err.Error()), "unique constraint"):
		writeError(w, http.StatusConflict, "username_exists", "username already exists", nil)
	case errors.Is(err, authsvc.ErrLastActiveAdmin):
		writeError(w, http.StatusConflict, "last_active_admin", err.Error(), nil)
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "user management operation failed", nil)
	}
}
