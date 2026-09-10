package web

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"kestrix/internal/config"
)

// TestWebhookMethodConfiguredByEnvironment reproduces the
// "Method configured by environment variable" scenario end to end:
// KESTRIX_WEBHOOK_METHOD=hmac with no --method flag must verify HMAC
// body signatures (200) and reject GitLab-token-only deliveries (401).
func TestWebhookMethodConfiguredByEnvironment(t *testing.T) {
	env := config.LoadEnvironment([]string{"KESTRIX_WEBHOOK_METHOD=hmac"})

	// Same idiom as cmd/kestrix/main.go cmdServe: no --method flag, so the
	// environment value applies.
	methodFlag := ""
	if methodFlag == "" {
		methodFlag = env.WebhookMethod
	}
	if methodFlag != "hmac" {
		t.Fatalf("resolved method = %q, want hmac (env fallback)", methodFlag)
	}

	const secret = "env-hmac-secret"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pipeline.tf"), []byte(pipelineWithTaskTF), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Config{
		ListenAddr:  ":0",
		PipelineDir: dir,
		Signature:   SignatureConfig{Secret: secret, Method: methodFlag},
	})
	testWebhookLog(t, srv)

	body := `{"object_kind":"push","ref":"refs/heads/main","checkout_sha":"abc123",` +
		`"project":{"id":4242,"path_with_namespace":"org/unknown","default_branch":"main"},` +
		`"commits":[{"id":"abc123","added":[],"modified":["svc/main.go"],"removed":[]}]}`

	// HMAC-signed delivery verifies.
	hmacReq := httptest.NewRequest("POST", "/webhook", io.NopCloser(bytes.NewBufferString(body)))
	hmacReq.Header.Set("X-Hub-Signature-256", computeHMAC([]byte(body), secret))
	hmacRec := httptest.NewRecorder()
	srv.router.ServeHTTP(hmacRec, hmacReq)
	if hmacRec.Code != http.StatusOK {
		t.Fatalf("HMAC delivery status = %d, want 200 (body=%s)", hmacRec.Code, hmacRec.Body.String())
	}

	// Same secret presented as a GitLab token must not verify under hmac.
	tokenReq := httptest.NewRequest("POST", "/webhook", io.NopCloser(bytes.NewBufferString(body)))
	tokenReq.Header.Set("X-Gitlab-Token", secret)
	tokenRec := httptest.NewRecorder()
	srv.router.ServeHTTP(tokenRec, tokenReq)
	if tokenRec.Code != http.StatusUnauthorized {
		t.Fatalf("token-only delivery status = %d, want 401 (body=%s)", tokenRec.Code, tokenRec.Body.String())
	}
}
