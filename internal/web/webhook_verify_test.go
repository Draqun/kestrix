package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kestrix/internal/domain"
)

// errReader returns a synthetic error on every Read. Used by
// login_throttle_test.go to verify that a body read failure is handled
// (e.g. a broken pipe from the agent) rather than panicking.
type errReader struct{}

func (errReader) Read(p []byte) (int, error) {
	_ = p
	return 0, errors.New("synthetic body read failure")
}

// TestWebhookBearerTokenDoesNotSubstituteForSharedSecret pins
// access-control's "Webhook secret is still enforced" scenario: a valid
// Kestrix user token is not a substitute for the webhook's own shared
// secret, and a wrong or missing secret is still rejected regardless of
// what else the request carries.
func TestWebhookBearerTokenDoesNotSubstituteForSharedSecret(t *testing.T) {
	srv := NewServer(Config{Signature: SignatureConfig{Secret: "correct"}})
	body := `{"ref":"refs/heads/main","commits":[],"repository":{"name":"x"}}`
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
	req.Header.Set("Authorization", authToken(t, srv, domain.RoleAdmin))
	// No X-Gitlab-Token at all: only a user bearer token is presented.
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (a user token must not substitute for the webhook secret)", w.Code)
	}
}

func TestVerifyGitLabToken(t *testing.T) {
	if verifyGitLabToken("", "my-secret") {
		t.Error("missing token should not verify")
	}
	if verifyGitLabToken("wrong-secret", "my-secret") {
		t.Error("wrong token should not verify")
	}
	if !verifyGitLabToken("my-secret", "my-secret") {
		t.Error("correct token should verify")
	}
	if verifyGitLabToken("my-secret", "") {
		t.Error("an empty secret should never verify")
	}
}

func computeHMAC(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyHMAC(t *testing.T) {
	secret := "webhook-secret"
	payload := []byte(`{"test":"data"}`)
	sig := computeHMAC(payload, secret)

	if verifyHMAC("", payload, secret) {
		t.Error("missing signature should not verify")
	}
	if verifyHMAC("sha256=deadbeef", payload, secret) {
		t.Error("wrong signature should not verify")
	}
	if !verifyHMAC(sig, payload, secret) {
		t.Error("correct signature should verify")
	}
	// Wrong algorithm prefix.
	if verifyHMAC("sha1=deadbeef", payload, secret) {
		t.Error("sha1 prefix should not verify")
	}
	// Missing '=' separator entirely.
	if verifyHMAC("garbage-no-equals", payload, secret) {
		t.Error("malformed signature should not verify")
	}
	// Valid prefix but invalid hex.
	if verifyHMAC("sha256=zzz", payload, secret) {
		t.Error("bad hex should not verify")
	}
	if verifyHMAC(sig, payload, "") {
		t.Error("an empty secret should never verify")
	}
}

func TestVerifyOne(t *testing.T) {
	payload := []byte(`{"test":"data"}`)
	sig := computeHMAC(payload, "s")

	h := http.Header{}
	h.Set("X-Gitlab-Token", "s")
	if !verifyOne("gitlab", h, payload, "s") {
		t.Error("gitlab method should verify a matching token")
	}
	if !verifyOne("", h, payload, "s") {
		t.Error("empty method should default to gitlab token comparison")
	}

	hmacHeader := http.Header{}
	hmacHeader.Set("X-Hub-Signature-256", sig)
	if !verifyOne("hmac", hmacHeader, payload, "s") {
		t.Error("hmac method should verify a matching signature")
	}
	// A matching token under the wrong header name for the configured
	// method must not verify - see the "HMAC method is honoured" scenario.
	if verifyOne("hmac", h, payload, "s") {
		t.Error("hmac method must not accept a matching X-Gitlab-Token")
	}
	if verifyOne("gitlab", hmacHeader, payload, "s") {
		t.Error("gitlab method must not accept a matching X-Hub-Signature-256")
	}
	if verifyOne("gitlab", h, payload, "") {
		t.Error("an empty secret should never verify")
	}
}

// TestVerifyDelivery pins design.md D1/D2: the project secret is tried
// first, the global secret is a fallback, and neither configured (or
// neither matching) rejects the delivery.
func TestVerifyDelivery(t *testing.T) {
	payload := []byte(`{"test":"data"}`)
	h := http.Header{}
	h.Set("X-Gitlab-Token", "project-secret")

	if !verifyDelivery("gitlab", h, payload, "project-secret", "global-secret") {
		t.Error("project secret match should verify")
	}

	global := http.Header{}
	global.Set("X-Gitlab-Token", "global-secret")
	if !verifyDelivery("gitlab", global, payload, "project-secret", "global-secret") {
		t.Error("global secret match should verify")
	}
	if !verifyDelivery("gitlab", global, payload, "", "global-secret") {
		t.Error("global secret match should verify when no project secret is set")
	}

	wrong := http.Header{}
	wrong.Set("X-Gitlab-Token", "wrong")
	if verifyDelivery("gitlab", wrong, payload, "project-secret", "") {
		t.Error("project set but wrong, and global absent, should not verify")
	}
	if verifyDelivery("gitlab", wrong, payload, "", "") {
		t.Error("both empty should not verify")
	}

	sig := computeHMAC(payload, "project-secret")
	hmacHeader := http.Header{}
	hmacHeader.Set("X-Hub-Signature-256", sig)
	if verifyDelivery("hmac", h, payload, "project-secret", "") {
		t.Error("hmac method should reject a matching X-Gitlab-Token")
	}
	tokenOnly := http.Header{}
	tokenOnly.Set("X-Gitlab-Token", "project-secret")
	if verifyDelivery("gitlab", hmacHeader, payload, "project-secret", "") {
		t.Error("gitlab method should reject a matching X-Hub-Signature-256")
	}
}

func TestProbeGitLabProjectID(t *testing.T) {
	if id := probeGitLabProjectID([]byte("not json")); id != 0 {
		t.Errorf("non-JSON body: id = %d, want 0", id)
	}
	if id := probeGitLabProjectID([]byte(`{"ref":"refs/heads/main"}`)); id != 0 {
		t.Errorf("body without an id: id = %d, want 0", id)
	}
	if id := probeGitLabProjectID([]byte(`{"project":{"id":42}}`)); id != 42 {
		t.Errorf("id = %d, want 42", id)
	}
}
