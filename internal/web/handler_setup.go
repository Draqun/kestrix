package web

import (
	"encoding/json"
	"errors"
	"net/http"

	"kestrix/internal/domain"
	"kestrix/internal/store"
)

// setupStatusResponse never carries account names or counts, only whether
// setup is still offered; see first-run-setup spec, "Setup status is
// discoverable without credentials".
type setupStatusResponse struct {
	Required bool `json:"required"`
}

// handleSetupStatus implements GET /api/setup. It is registered on the
// public router group: no token is required to call it.
//
// @Summary Report whether first-run setup is required
// @Tags setup
// @Produce json
// @Success 200 {object} setupStatusResponse
// @Router /api/setup [get]
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	userStore := s.requireUserStore(w)
	if userStore == nil {
		return
	}
	hasAny, err := userStore.HasAnyUser()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(setupStatusResponse{Required: !hasAny})
}

// setupRequest is the POST /api/setup body.
type setupRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

// handleCompleteSetup implements POST /api/setup. It is registered on the
// public router group, throttled per client address (see throttleSetup),
// and creates the first account with the admin role only while no account
// exists - see first-run-setup spec, "First administrator can be created
// once" and design.md D1/D3.
//
// @Summary Create the first administrator account
// @Tags setup
// @Accept json
// @Produce json
// @Param body body setupRequest true "First administrator"
// @Success 201 {object} userView
// @Failure 409 {object} map[string]string
// @Router /api/setup [post]
func (s *Server) handleCompleteSetup(w http.ResponseWriter, r *http.Request) {
	userStore := s.requireUserStore(w)
	if userStore == nil {
		return
	}
	if !s.requirePasswordHasher(w) {
		return
	}
	var req setupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "name and password are required")
		return
	}
	hash, ok := s.hashNewPassword(w, req.Password)
	if !ok {
		return
	}
	u, err := userStore.CreateFirstUser(req.Name, domain.RoleAdmin, hash)
	if err != nil {
		if errors.Is(err, store.ErrSetupDone) {
			writeError(w, http.StatusConflict, "setup already completed")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// See handleCreateUser's identical call: a request naming this account
	// may have cached a negative identity result before it existed.
	s.invalidateIdentity(u.Name)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(s.userViewFor(u))
}
