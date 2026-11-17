package web

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"kestrix/internal/domain"
)

func (s *Server) requireCredentialStore(w http.ResponseWriter) domain.CredentialStore {
	if s.credentialStore == nil {
		writeError(w, http.StatusServiceUnavailable, "credential store is not configured")
		return nil
	}
	return s.credentialStore
}

// handleListCredentials godoc
// @Summary List stored credentials metadata
// @Description Returns metadata for all registered credentials without revealing secret values.
// @Tags credentials
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /api/credentials [get]
func (s *Server) handleListCredentials(w http.ResponseWriter, _ *http.Request) {
	store := s.requireCredentialStore(w)
	if store == nil {
		return
	}
	items, err := store.ListCredentialMetadata()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": items})
}

// handleCreateCredential godoc
// @Summary Create a credential
// @Description Encrypts and stores a new credential, returning its safe metadata.
// @Tags credentials
// @Accept json
// @Produce json
// @Param body body domain.CredentialInput true "Credential definition and secret material"
// @Success 201 {object} domain.CredentialMetadata
// @Failure 400 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /api/credentials [post]
func (s *Server) handleCreateCredential(w http.ResponseWriter, r *http.Request) {
	store := s.requireCredentialStore(w)
	if store == nil {
		return
	}
	var input domain.CredentialInput
	if !decodeJSON(w, r, &input) {
		return
	}
	meta, err := store.CreateCredential(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(meta)
}

// handleGetCredential godoc
// @Summary Get credential metadata by ref
// @Description Returns safe metadata for a credential by its reference without revealing secret values.
// @Tags credentials
// @Produce json
// @Param ref path string true "Credential reference"
// @Success 200 {object} domain.CredentialMetadata
// @Failure 404 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /api/credentials/{ref} [get]
func (s *Server) handleGetCredential(w http.ResponseWriter, r *http.Request) {
	store := s.requireCredentialStore(w)
	if store == nil {
		return
	}
	meta, err := store.GetCredentialMetadata(chi.URLParam(r, "ref"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(meta)
}

// handleRotateCredential godoc
// @Summary Rotate credential secret material
// @Description Updates the secret material of an existing credential.
// @Tags credentials
// @Accept json
// @Produce json
// @Param ref path string true "Credential reference"
// @Param body body domain.CredentialInput true "Updated credential secret material"
// @Success 200 {object} domain.CredentialMetadata
// @Failure 400 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /api/credentials/{ref} [put]
func (s *Server) handleRotateCredential(w http.ResponseWriter, r *http.Request) {
	store := s.requireCredentialStore(w)
	if store == nil {
		return
	}
	var input domain.CredentialInput
	if !decodeJSON(w, r, &input) {
		return
	}
	meta, err := store.RotateCredential(chi.URLParam(r, "ref"), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(meta)
}

// handleRevokeCredential godoc
// @Summary Revoke a credential
// @Description Revokes a credential reference so it cannot be used for subsequent runs.
// @Tags credentials
// @Param ref path string true "Credential reference"
// @Success 204 "Credential revoked"
// @Failure 404 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /api/credentials/{ref}/revoke [post]
func (s *Server) handleRevokeCredential(w http.ResponseWriter, r *http.Request) {
	store := s.requireCredentialStore(w)
	if store == nil {
		return
	}
	if err := store.RevokeCredential(chi.URLParam(r, "ref")); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
