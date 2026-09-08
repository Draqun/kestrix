package web

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"kestrix/internal/domain"
	"kestrix/internal/store"
)

// userView is what account administration returns: identity, role, and
// grants, never a password hash.
type userView struct {
	domain.User
	Grants []domain.ProjectGrant `json:"grants,omitempty"`
}

func (s *Server) requireUserStore(w http.ResponseWriter) domain.UserStore {
	if s.userStore == nil {
		writeError(w, http.StatusServiceUnavailable, "user store is not configured")
		return nil
	}
	return s.userStore
}

func (s *Server) userViewFor(u domain.User) userView {
	view := userView{User: u}
	if s.grantStore != nil {
		if grants, err := s.grantStore.ListGrantsForUser(u.Name); err == nil {
			view.Grants = grants
		}
	}
	return view
}

// handleListUsers implements GET /api/users (admin only).
//
// @Summary List accounts
// @Tags users
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/users [get]
func (s *Server) handleListUsers(w http.ResponseWriter, _ *http.Request) {
	userStore := s.requireUserStore(w)
	if userStore == nil {
		return
	}
	users, err := userStore.ListUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	views := make([]userView, len(users))
	for i, u := range users {
		views[i] = s.userViewFor(u)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": views})
}

// createUserRequest is the POST /api/users body.
type createUserRequest struct {
	Name     string      `json:"name"`
	Password string      `json:"password"`
	Role     domain.Role `json:"role"`
}

// handleCreateUser implements POST /api/users (admin only).
//
// @Summary Create an account
// @Tags users
// @Accept json
// @Produce json
// @Param body body createUserRequest true "New account"
// @Success 201 {object} userView
// @Router /api/users [post]
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	userStore := s.requireUserStore(w)
	if userStore == nil {
		return
	}
	if s.passwordHasher == nil {
		writeError(w, http.StatusServiceUnavailable, "password hashing is not configured")
		return
	}
	var req createUserRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "name and password are required")
		return
	}
	if !req.Role.Valid() {
		writeError(w, http.StatusBadRequest, "role must be viewer, operator, or admin")
		return
	}
	hash, err := s.passwordHasher.Hash(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "hash password")
		return
	}
	u, err := userStore.CreateUser(req.Name, req.Role, hash)
	if err != nil {
		if errors.Is(err, store.ErrUserExists) {
			writeError(w, http.StatusConflict, "user already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// A request naming this account (a valid or forged bearer token) may
	// have cached a negative identity result before it existed; without
	// this, the new account would be rejected as unknown until that cache
	// entry expires. See resolveIdentity in middleware.go.
	s.invalidateIdentity(u.Name)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(s.userViewFor(u))
}

// updateUserRoleRequest is the PUT /api/users/{user}/role body.
type updateUserRoleRequest struct {
	Role domain.Role `json:"role"`
}

// handleUpdateUserRole implements PUT /api/users/{user}/role (admin only).
//
// @Summary Change an account's global role
// @Tags users
// @Accept json
// @Produce json
// @Param user path string true "Account name"
// @Param body body updateUserRoleRequest true "New role"
// @Success 200 {object} userView
// @Router /api/users/{user}/role [put]
func (s *Server) handleUpdateUserRole(w http.ResponseWriter, r *http.Request) {
	userStore := s.requireUserStore(w)
	if userStore == nil {
		return
	}
	name := chi.URLParam(r, "user")
	var req updateUserRoleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !req.Role.Valid() {
		writeError(w, http.StatusBadRequest, "role must be viewer, operator, or admin")
		return
	}
	if err := userStore.UpdateUserRole(name, req.Role); err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.invalidateIdentity(name)
	u, err := userStore.GetUser(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.userViewFor(u))
}

// setGrantsRequest is the PUT /api/users/{user}/grants body: the full,
// desired set of project grants for the account. Any existing grant on a
// project not listed here is removed.
type setGrantsRequest struct {
	Grants []struct {
		ProjectName string      `json:"project_name"`
		Role        domain.Role `json:"role"`
	} `json:"grants"`
}

// handleSetUserGrants implements PUT /api/users/{user}/grants (admin only).
//
// @Summary Replace an account's project grants
// @Tags users
// @Accept json
// @Produce json
// @Param user path string true "Account name"
// @Param body body setGrantsRequest true "Desired grants"
// @Success 200 {object} userView
// @Router /api/users/{user}/grants [put]
func (s *Server) handleSetUserGrants(w http.ResponseWriter, r *http.Request) {
	userStore := s.requireUserStore(w)
	if userStore == nil {
		return
	}
	if s.grantStore == nil {
		writeError(w, http.StatusServiceUnavailable, "grant store is not configured")
		return
	}
	name := chi.URLParam(r, "user")
	if _, err := userStore.GetUser(name); err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	var req setGrantsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	for _, g := range req.Grants {
		if !g.Role.Valid() {
			writeError(w, http.StatusBadRequest, "each grant role must be viewer, operator, or admin")
			return
		}
	}
	existing, err := s.grantStore.ListGrantsForUser(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	desired := make(map[string]domain.Role, len(req.Grants))
	for _, g := range req.Grants {
		desired[g.ProjectName] = g.Role
	}
	for _, g := range existing {
		if _, keep := desired[g.ProjectName]; !keep {
			if err := s.grantStore.RemoveProjectGrant(name, g.ProjectName); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}
	for projectName, role := range desired {
		if err := s.grantStore.SetProjectGrant(name, projectName, role); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	u, err := userStore.GetUser(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.userViewFor(u))
}

// handleDeleteUser implements DELETE /api/users/{user} (admin only).
//
// @Summary Delete an account
// @Tags users
// @Param user path string true "Account name"
// @Success 204
// @Router /api/users/{user} [delete]
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	userStore := s.requireUserStore(w)
	if userStore == nil {
		return
	}
	name := chi.URLParam(r, "user")
	if err := userStore.DeleteUser(name); err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.invalidateIdentity(name)
	w.WriteHeader(http.StatusNoContent)
}
