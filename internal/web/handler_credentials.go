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
