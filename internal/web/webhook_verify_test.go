package web

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifyGitLabToken(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	middleware := VerifyGitLabSignature("my-secret")
	wrapped := middleware(handler)

	// Missing token → 403
	req := httptest.NewRequest("POST", "/webhook", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("missing token: status = %d, want 403", w.Code)
	}

	// Wrong token → 403
	req = httptest.NewRequest("POST", "/webhook", nil)
	req.Header.Set("X-Gitlab-Token", "wrong-secret")
	w = httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("wrong token: status = %d, want 403", w.Code)
	}

	// Correct token → 200
	req = httptest.NewRequest("POST", "/webhook", nil)
	req.Header.Set("X-Gitlab-Token", "my-secret")
	w = httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("correct token: status = %d, want 200", w.Code)
	}
}

func TestVerifyHMACSignature(t *testing.T) {
	secret := "webhook-secret"
	payload := []byte(`{"test":"data"}`)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	middleware := VerifyHMACSignature(secret)
	wrapped := middleware(handler)

	// Compute correct signature
	sig := computeHMAC(payload, secret)

	// Missing signature → 403
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(payload))
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("missing sig: status = %d, want 403", w.Code)
	}

	// Wrong signature → 403
	req = httptest.NewRequest("POST", "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	w = httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("wrong sig: status = %d, want 403", w.Code)
	}

	// Correct signature → 200
	req = httptest.NewRequest("POST", "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", sig)
	w = httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("correct sig: status = %d, want 200", w.Code)
	}
}

func TestSignatureConfig(t *testing.T) {
	// Disabled → nil middleware
	cfg := SignatureConfig{Enabled: false}
	mw, err := cfg.Middleware()
	if err != nil || mw != nil {
		t.Error("disabled config should return nil")
	}

	// Enabled but no secret → error
	cfg = SignatureConfig{Enabled: true}
	_, err = cfg.Middleware()
	if err == nil {
		t.Error("enabled without secret should error")
	}

	// GitLab method
	cfg = SignatureConfig{Enabled: true, Secret: "s", Method: "gitlab"}
	mw, err = cfg.Middleware()
	if err != nil || mw == nil {
		t.Error("gitlab method should return middleware")
	}

	// Unknown method → error
	cfg = SignatureConfig{Enabled: true, Secret: "s", Method: "unknown"}
	_, err = cfg.Middleware()
	if err == nil {
		t.Error("unknown method should error")
	}
}

func computeHMAC(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
