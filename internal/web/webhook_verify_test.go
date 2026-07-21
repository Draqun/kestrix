package web

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

	// Wrong algorithm prefix (sha1=<hex>) → 403 "invalid signature format".
	// Exercises the `parts[0] != "sha256"` branch of VerifyHMACSignature.
	req = httptest.NewRequest("POST", "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", "sha1=deadbeef")
	w = httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("sha1 prefix: status = %d, want 403", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid signature format") {
		t.Errorf("sha1 prefix: body = %q, want invalid signature format error", w.Body.String())
	}

	// Missing '=' separator entirely → 403 "invalid signature format".
	req = httptest.NewRequest("POST", "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", "garbage-no-equals")
	w = httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("malformed signature: status = %d, want 403", w.Code)
	}

	// Valid prefix but invalid hex → 403 "invalid signature encoding".
	// Exercises the `hex.DecodeString` error branch.
	req = httptest.NewRequest("POST", "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", "sha256=zzz")
	w = httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("bad hex: status = %d, want 403", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid signature encoding") {
		t.Errorf("bad hex: body = %q, want invalid signature encoding error", w.Body.String())
	}

	// Body read error → 500. Inject a request whose body fails on Read so
	// io.ReadAll returns an error, exercising that branch. The signature
	// header itself must still be present and well-formed; reading the
	// body happens after the hex decode, so a matching-but-non-decodable
	// hash is what triggers the body stage.
	failingHandler := VerifyHMACSignature(secret)
	failingWrapped := failingHandler(handler)
	req = httptest.NewRequest("POST", "/webhook", errReader{})
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	w = httptest.NewRecorder()
	failingWrapped.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("body read failure: status = %d, want 500", w.Code)
	}
	if !strings.Contains(w.Body.String(), "failed to read body") {
		t.Errorf("body read failure: body = %q, want failed to read body error", w.Body.String())
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

	// HMAC method → returns middleware. The hmac branch was uncovered
	// because the existing test only exercised the gitlab method.
	cfg = SignatureConfig{Enabled: true, Secret: "s", Method: "hmac"}
	mw, err = cfg.Middleware()
	if err != nil {
		t.Errorf("hmac method should not error: %v", err)
	}
	if mw == nil {
		t.Error("hmac method should return middleware")
	}
}

func computeHMAC(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// errReader returns a synthetic error on every Read. Used to verify that
// VerifyHMACSignature handles a body read failure (e.g. a broken pipe from
// the agent) rather than panicking on the io.ReadAll path.
type errReader struct {
	read bool
}

func (errReader) Read(p []byte) (int, error) {
	return 0, errors.New("synthetic body read failure")
}
