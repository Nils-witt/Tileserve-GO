package webserver

import (
	"errors"
	"fmt"
	"net/http"

	"nilswitt.dev/tileserve-go/internal/auth"
	"nilswitt.dev/tileserve-go/internal/httputil"
	"nilswitt.dev/tileserve-go/internal/store"
	"nilswitt.dev/tileserve-go/internal/webserver/auditlog"
)

type userRequest struct {
	Username            string `json:"username"`
	Password            string `json:"password"`
	CanCreate           bool   `json:"canCreate"`
	CanEdit             bool   `json:"canEdit"`
	CanDelete           bool   `json:"canDelete"`
	CanEditGeoObjects   bool   `json:"canEditGeoObjects"`
	CanDeleteGeoObjects bool   `json:"canDeleteGeoObjects"`
	CanViewAll          bool   `json:"canViewAll"`
	IsAdmin             bool   `json:"isAdmin"`
}

// permissions extracts the global Permissions fields carried by a userRequest.
func (req userRequest) permissions() store.Permissions {
	return store.Permissions{
		CanCreate:           req.CanCreate,
		CanEdit:             req.CanEdit,
		CanDelete:           req.CanDelete,
		CanEditGeoObjects:   req.CanEditGeoObjects,
		CanDeleteGeoObjects: req.CanDeleteGeoObjects,
		CanViewAll:          req.CanViewAll,
		IsAdmin:             req.IsAdmin,
	}
}

// userFilterFromQuery builds a store.UserFilter from r's query parameters,
// writing a 400 response and returning ok=false if any of the boolean
// params is malformed.
func userFilterFromQuery(w http.ResponseWriter, r *http.Request) (filter store.UserFilter, ok bool) {
	isAdmin, ok := httputil.QueryBoolParam(w, r, "isAdmin")
	if !ok {
		return store.UserFilter{}, false
	}

	canCreate, ok := httputil.QueryBoolParam(w, r, "canCreate")
	if !ok {
		return store.UserFilter{}, false
	}

	canEdit, ok := httputil.QueryBoolParam(w, r, "canEdit")
	if !ok {
		return store.UserFilter{}, false
	}

	canDelete, ok := httputil.QueryBoolParam(w, r, "canDelete")
	if !ok {
		return store.UserFilter{}, false
	}

	canEditGeoObjects, ok := httputil.QueryBoolParam(w, r, "canEditGeoObjects")
	if !ok {
		return store.UserFilter{}, false
	}

	canDeleteGeoObjects, ok := httputil.QueryBoolParam(w, r, "canDeleteGeoObjects")
	if !ok {
		return store.UserFilter{}, false
	}

	canViewAll, ok := httputil.QueryBoolParam(w, r, "canViewAll")
	if !ok {
		return store.UserFilter{}, false
	}

	return store.UserFilter{
		Search:              r.URL.Query().Get("search"),
		IsAdmin:             isAdmin,
		CanCreate:           canCreate,
		CanEdit:             canEdit,
		CanDelete:           canDelete,
		CanEditGeoObjects:   canEditGeoObjects,
		CanDeleteGeoObjects: canDeleteGeoObjects,
		CanViewAll:          canViewAll,
	}, true
}

// UsersListHandler serves GET /users: lists all users. Open to any
// authenticated user (e.g. so a map owner can pick a username to grant a
// per-map permission to, or to transfer ownership to — see
// MapPermissionSetHandler and MapOwnerSetHandler).
func UsersListHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter, ok := userFilterFromQuery(w, r)
		if !ok {
			return
		}

		users, err := st.ListUsers(r.Context(), filter)
		if err != nil {
			http.Error(w, "failed to list users", http.StatusInternalServerError)
			return
		}

		httputil.WriteJSON(w, http.StatusOK, users)
	}
}

// UserCreateHandler serves POST /users (admin-only): creates a new user.
func UserCreateHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req userRequest
		if !httputil.DecodeJSON(w, r, &req) {
			return
		}

		if req.Username == "" || req.Password == "" {
			http.Error(w, "username and password are required", http.StatusBadRequest)
			return
		}

		u, err := st.CreateUser(r.Context(), req.Username, req.Password, req.permissions())
		if err != nil {
			writeStoreError(w, err, store.ErrUserExists, http.StatusConflict, "user already exists", "failed to create user")
			return
		}

		auditlog.RecordAudit(r, st, "create", "user", u.Username, fmt.Sprintf("isAdmin=%v", u.IsAdmin))

		httputil.WriteJSON(w, http.StatusCreated, u)
	}
}

// UserUpdateHandler serves PUT /users/{username} (admin-only): updates the
// user's permissions (and password, if given).
func UserUpdateHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := r.PathValue("username")

		var req userRequest
		if !httputil.DecodeJSON(w, r, &req) {
			return
		}

		u, err := st.UpdateUser(r.Context(), username, req.permissions(), req.Password)
		if err != nil {
			writeStoreError(w, err, store.ErrUserNotFound, http.StatusNotFound, "user not found", "failed to update user")
			return
		}

		auditlog.RecordAudit(r, st, "update", "user", u.Username, fmt.Sprintf("isAdmin=%v passwordChanged=%v", u.IsAdmin, req.Password != ""))

		httputil.WriteJSON(w, http.StatusOK, u)
	}
}

// UserDeleteHandler serves DELETE /users/{username} (admin-only): removes
// the user (an admin may not delete their own account).
func UserDeleteHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := r.PathValue("username")

		if username == auth.UsernameFromContext(r.Context()) {
			http.Error(w, "cannot delete your own account", http.StatusBadRequest)
			return
		}

		if err := st.DeleteUser(r.Context(), username); err != nil {
			if errors.Is(err, store.ErrUserOwnsMaps) {
				http.Error(w, "user still owns one or more maps; transfer ownership first", http.StatusConflict)
				return
			}

			writeStoreError(w, err, store.ErrUserNotFound, http.StatusNotFound, "user not found", "failed to delete user")

			return
		}

		auditlog.RecordAudit(r, st, "delete", "user", username, "")

		w.WriteHeader(http.StatusNoContent)
	}
}
