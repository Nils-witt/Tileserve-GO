package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"nilswitt.dev/tileserve-go/internal/store"
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

// requireAdmin is a shorthand for requiring the is_admin permission.
func requireAdmin(w http.ResponseWriter, r *http.Request, st *store.Store) bool {
	return requirePermission(w, r, st, func(p store.Permissions) bool { return p.IsAdmin })
}

// userFilterFromQuery builds a store.UserFilter from r's query parameters,
// writing a 400 response and returning ok=false if any of the boolean
// params is malformed.
func userFilterFromQuery(w http.ResponseWriter, r *http.Request) (filter store.UserFilter, ok bool) {
	isAdmin, ok := queryBoolParam(w, r, "isAdmin")
	if !ok {
		return store.UserFilter{}, false
	}

	canCreate, ok := queryBoolParam(w, r, "canCreate")
	if !ok {
		return store.UserFilter{}, false
	}

	canEdit, ok := queryBoolParam(w, r, "canEdit")
	if !ok {
		return store.UserFilter{}, false
	}

	canDelete, ok := queryBoolParam(w, r, "canDelete")
	if !ok {
		return store.UserFilter{}, false
	}

	canEditGeoObjects, ok := queryBoolParam(w, r, "canEditGeoObjects")
	if !ok {
		return store.UserFilter{}, false
	}

	canDeleteGeoObjects, ok := queryBoolParam(w, r, "canDeleteGeoObjects")
	if !ok {
		return store.UserFilter{}, false
	}

	canViewAll, ok := queryBoolParam(w, r, "canViewAll")
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

// UsersCollectionHandler serves the /users collection route: GET lists all
// users and is open to any authenticated user (e.g. so a map owner can pick
// a username to grant a per-map permission to, or to transfer ownership to
// — see mapPermissionItemHandler and mapOwnerItemHandler), while POST
// creates a new one and remains admin-only.
func UsersCollectionHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAuthenticated(w, r) {
			return
		}

		switch r.Method {
		case http.MethodGet:
			filter, ok := userFilterFromQuery(w, r)
			if !ok {
				return
			}

			users, err := st.ListUsers(r.Context(), filter)
			if err != nil {
				http.Error(w, "failed to list users", http.StatusInternalServerError)
				return
			}

			writeJSON(w, http.StatusOK, users)

		case http.MethodPost:
			if !requireAdmin(w, r, st) {
				return
			}

			var req userRequest
			if !decodeJSON(w, r, &req) {
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

			recordAudit(r, st, "create", "user", u.Username, fmt.Sprintf("isAdmin=%v", u.IsAdmin))

			writeJSON(w, http.StatusCreated, u)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// UserItemHandler serves the /users/{username} item route (admin-only): PUT
// updates the user's permissions (and password, if given), DELETE removes
// the user (an admin may not delete their own account). It also dispatches
// the nested /users/{username}/api-keys[/{id}] sub-resource, mirroring
// MapsItemHandler's path-segment dispatch style.
func UserItemHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/users/"), "/")
		segments := strings.Split(path, "/")

		username := segments[0]
		if username == "" {
			http.Error(w, "invalid username", http.StatusBadRequest)
			return
		}

		if !requireAdmin(w, r, st) {
			return
		}

		if len(segments) >= 2 && segments[1] == "api-keys" {
			routeUserAPIKeys(w, r, st, username, segments)
			return
		}

		if len(segments) != 1 {
			http.NotFound(w, r)
			return
		}

		switch r.Method {
		case http.MethodPut:
			var req userRequest
			if !decodeJSON(w, r, &req) {
				return
			}

			u, err := st.UpdateUser(r.Context(), username, req.permissions(), req.Password)
			if err != nil {
				writeStoreError(w, err, store.ErrUserNotFound, http.StatusNotFound, "user not found", "failed to update user")
				return
			}

			recordAudit(r, st, "update", "user", u.Username, fmt.Sprintf("isAdmin=%v passwordChanged=%v", u.IsAdmin, req.Password != ""))

			writeJSON(w, http.StatusOK, u)

		case http.MethodDelete:
			if username == usernameFromContext(r.Context()) {
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

			recordAudit(r, st, "delete", "user", username, "")

			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}
