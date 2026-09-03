package handler

import (
	"errors"
	"fmt"
	"net/http"

	"nilswitt.dev/tileserve-go/internal/handler/auditlog"
	"nilswitt.dev/tileserve-go/internal/handler/utils"
	"nilswitt.dev/tileserve-go/internal/store"
)

type groupRequest struct {
	Name                string `json:"name"`
	CanCreate           bool   `json:"canCreate"`
	CanEdit             bool   `json:"canEdit"`
	CanDelete           bool   `json:"canDelete"`
	CanEditGeoObjects   bool   `json:"canEditGeoObjects"`
	CanDeleteGeoObjects bool   `json:"canDeleteGeoObjects"`
	CanViewAll          bool   `json:"canViewAll"`
	IsAdmin             bool   `json:"isAdmin"`
	LDAPGroupDN         string `json:"ldapGroupDn"`
	OIDCGroupClaim      string `json:"oidcGroupClaim"`
}

// permissions extracts the global Permissions fields carried by a groupRequest.
func (req groupRequest) permissions() store.Permissions {
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

// groupFilterFromQuery builds a store.GroupFilter from r's query parameters.
func groupFilterFromQuery(r *http.Request) store.GroupFilter {
	return store.GroupFilter{Search: r.URL.Query().Get("search")}
}

// GroupsListHandler serves GET /groups: lists all groups. Open to any
// authenticated user, same rationale as UsersListHandler (e.g. so a map
// owner can pick a group to grant a per-map permission to).
func GroupsListHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groups, err := st.ListGroups(r.Context(), groupFilterFromQuery(r))
		if err != nil {
			http.Error(w, "failed to list groups", http.StatusInternalServerError)
			return
		}

		utils.WriteJSON(w, http.StatusOK, groups)
	}
}

// GroupGetHandler serves GET /groups/{id}: fetches a single group. Open to
// any authenticated user, same rationale as GroupsListHandler.
func GroupGetHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := utils.PathUUID(w, r, "id", "group id")
		if !ok {
			return
		}

		g, err := st.GetGroup(r.Context(), id)
		if err != nil {
			writeStoreError(w, err, store.ErrGroupNotFound, http.StatusNotFound, "group not found", "failed to get group")
			return
		}

		utils.WriteJSON(w, http.StatusOK, g)
	}
}

// GroupCreateHandler serves POST /groups (admin-only): creates a new group.
func GroupCreateHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req groupRequest
		if !utils.DecodeJSON(w, r, &req) {
			return
		}

		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}

		g, err := st.CreateGroup(r.Context(), req.Name, req.permissions(), req.LDAPGroupDN, req.OIDCGroupClaim, usernameFromContext(r.Context()))
		if err != nil {
			writeStoreError(w, err, store.ErrGroupExists, http.StatusConflict, "group already exists", "failed to create group")
			return
		}

		auditlog.RecordAudit(r, st, "create", "group", g.ID.String(), fmt.Sprintf("name=%s isAdmin=%v", g.Name, g.IsAdmin))

		utils.WriteJSON(w, http.StatusCreated, g)
	}
}

// GroupUpdateHandler serves PUT /groups/{id} (admin-only): updates the
// group's name, permission bundle, and LDAP/OIDC linking fields.
func GroupUpdateHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := utils.PathUUID(w, r, "id", "group id")
		if !ok {
			return
		}

		var req groupRequest
		if !utils.DecodeJSON(w, r, &req) {
			return
		}

		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}

		g, err := st.UpdateGroup(r.Context(), id, req.Name, req.permissions(), req.LDAPGroupDN, req.OIDCGroupClaim, usernameFromContext(r.Context()))
		if err != nil {
			if errors.Is(err, store.ErrGroupExists) {
				http.Error(w, "group already exists", http.StatusConflict)
				return
			}

			writeStoreError(w, err, store.ErrGroupNotFound, http.StatusNotFound, "group not found", "failed to update group")

			return
		}

		auditlog.RecordAudit(r, st, "update", "group", g.ID.String(), fmt.Sprintf("name=%s isAdmin=%v", g.Name, g.IsAdmin))

		utils.WriteJSON(w, http.StatusOK, g)
	}
}

// GroupDeleteHandler serves DELETE /groups/{id} (admin-only): removes the
// group. Its memberships and per-map grants are removed via ON DELETE
// CASCADE.
//
//nolint:dupl // structurally the same simple "delete by id" handler as SyncRemoteDeleteHandler; each entity's own delete endpoint follows this same idiom throughout the package, and a shared generic helper isn't worth the indirection for a handful of lines
func GroupDeleteHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := utils.PathUUID(w, r, "id", "group id")
		if !ok {
			return
		}

		if err := st.DeleteGroup(r.Context(), id); err != nil {
			writeStoreError(w, err, store.ErrGroupNotFound, http.StatusNotFound, "group not found", "failed to delete group")
			return
		}

		auditlog.RecordAudit(r, st, "delete", "group", id.String(), "")

		w.WriteHeader(http.StatusNoContent)
	}
}
