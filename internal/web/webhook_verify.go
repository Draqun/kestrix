package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// VerifyGitLabSignature is middleware that verifies the X-Gitlab-Token header
// against a shared secret. If the token doesn't match, the request is rejected
// with 403 Forbidden.
//
// GitLab sends the token as a plain header (not HMAC), so we compare directly.
// For GitHub-style HMAC-SHA256 verification, use VerifyHMACSignature instead.
func VerifyGitLabSignature(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("X-Gitlab-Token")
			if token == "" {
				log.Printf("webhook: missing X-Gitlab-Token header")
				http.Error(w, `{"error":"missing X-Gitlab-Token"}`, http.StatusForbidden)
				return
			}
			if !hmac.Equal([]byte(token), []byte(secret)) {
				log.Printf("webhook: invalid X-Gitlab-Token")
				http.Error(w, `{"error":"invalid X-Gitlab-Token"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// VerifyHMACSignature is middleware that verifies HMAC-SHA256 signatures
// (used by GitHub and compatible webhooks).
//
// The signature is expected in the format: "sha256=<hex>".
// The secret is the raw bytes of the HMAC key.
func VerifyHMACSignature(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sigHeader := r.Header.Get("X-Hub-Signature-256")
			if sigHeader == "" {
				log.Printf("webhook: missing X-Hub-Signature-256 header")
				http.Error(w, `{"error":"missing signature"}`, http.StatusForbidden)
				return
			}

			// Parse "sha256=<hex>"
			parts := strings.SplitN(sigHeader, "=", 2)
			if len(parts) != 2 || parts[0] != "sha256" {
				log.Printf("webhook: invalid signature format: %s", sigHeader)
				http.Error(w, `{"error":"invalid signature format"}`, http.StatusForbidden)
				return
			}

			expectedSig, err := hex.DecodeString(parts[1])
			if err != nil {
				log.Printf("webhook: invalid hex in signature")
				http.Error(w, `{"error":"invalid signature encoding"}`, http.StatusForbidden)
				return
			}

			// Read body for HMAC computation (we need to re-set it for the next handler).
			body, err := io.ReadAll(r.Body)
			if err != nil {
				log.Printf("webhook: failed to read body for verification: %v", err)
				http.Error(w, `{"error":"failed to read body"}`, http.StatusInternalServerError)
				return
			}
			r.Body.Close()

			// Compute HMAC-SHA256.
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write(body)
			computedSig := mac.Sum(nil)

			if !hmac.Equal(computedSig, expectedSig) {
				log.Printf("webhook: HMAC mismatch")
				http.Error(w, `{"error":"invalid signature"}`, http.StatusForbidden)
				return
			}

			// Restore body for downstream handlers.
			r.Body = io.NopCloser(strings.NewReader(string(body)))
			next.ServeHTTP(w, r)
		})
	}
}

// SignatureConfig holds the webhook signature verification configuration.
type SignatureConfig struct {
	// Enabled turns verification on/off.
	Enabled bool

	// Secret is the shared secret for token/HMAC verification.
	Secret string

	// Method determines which verification method to use.
	// "gitlab" = X-Gitlab-Token header comparison
	// "hmac"   = HMAC-SHA256 signature verification
	Method string
}

// Middleware returns the appropriate verification middleware based on config.
func (c SignatureConfig) Middleware() (func(http.Handler) http.Handler, error) {
	if !c.Enabled {
		return nil, nil
	}
	if c.Secret == "" {
		return nil, fmt.Errorf("webhook secret is required when verification is enabled")
	}
	switch c.Method {
	case "gitlab", "":
		return VerifyGitLabSignature(c.Secret), nil
	case "hmac":
		return VerifyHMACSignature(c.Secret), nil
	default:
		return nil, fmt.Errorf("unknown verification method %q (use 'gitlab' or 'hmac')", c.Method)
	}
}
