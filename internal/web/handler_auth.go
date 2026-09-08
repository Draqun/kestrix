package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"kestrix/internal/service"
)

// loginRequest is the POST /api/auth/login body.
type loginRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

// loginResponse never carries a password hash: it is the token and its
// expiry only.
type loginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// handleLogin implements POST /api/auth/login. It is registered on the
// public router group: no token is required to call it.
//
// @Summary Authenticate and obtain a token
// @Tags auth
// @Accept json
// @Produce json
// @Param body body loginRequest true "Credentials"
// @Success 200 {object} loginResponse
// @Failure 401 {object} map[string]string
// @Router /api/auth/login [post]
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.authService == nil {
		writeError(w, http.StatusServiceUnavailable, "authentication is not configured")
		return
	}
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	token, expiresAt, err := s.authService.Login(req.Name, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid name or password")
			return
		}
		writeError(w, http.StatusInternalServerError, "login failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(loginResponse{Token: token, ExpiresAt: expiresAt})
}
