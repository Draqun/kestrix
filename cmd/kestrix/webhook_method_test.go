package main

import (
	"testing"

	"kestrix/internal/config"
	"kestrix/internal/web"
)

// TestResolveWebhookMethodEnvFallback pins the --method /
// KESTRIX_WEBHOOK_METHOD equivalence: when --method is unset the server
// must verify with the env value, and the flag wins when both are set.
func TestResolveWebhookMethodEnvFallback(t *testing.T) {
	env := config.LoadEnvironment([]string{"KESTRIX_WEBHOOK_METHOD=hmac"})

	// No flag: Config.Signature.Method reflects the env var.
	method := resolveWebhookMethod("", env)
	cfg := web.Config{Signature: web.SignatureConfig{Method: method}}
	if cfg.Signature.Method != "hmac" {
		t.Fatalf("Signature.Method = %q, want %q (env fallback)", cfg.Signature.Method, "hmac")
	}

	// Flag wins over env.
	method = resolveWebhookMethod("gitlab", env)
	cfg = web.Config{Signature: web.SignatureConfig{Method: method}}
	if cfg.Signature.Method != "gitlab" {
		t.Fatalf("Signature.Method = %q, want %q (flag precedence)", cfg.Signature.Method, "gitlab")
	}

	// Neither set: empty, so verifyOne defaults to gitlab.
	emptyEnv := config.LoadEnvironment(nil)
	method = resolveWebhookMethod("", emptyEnv)
	cfg = web.Config{Signature: web.SignatureConfig{Method: method}}
	if cfg.Signature.Method != "" {
		t.Fatalf("Signature.Method = %q, want empty (gitlab default)", cfg.Signature.Method)
	}

	// Nil environment never panics and yields the default.
	method = resolveWebhookMethod("", nil)
	if method != "" {
		t.Fatalf("resolveWebhookMethod(\"\", nil) = %q, want empty", method)
	}
}
