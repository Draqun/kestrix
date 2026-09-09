package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// verifyGitLabToken reports whether header is the X-Gitlab-Token value
// GitLab would send for secret, compared in constant time. An empty secret
// never verifies: callers must not invoke this with an unconfigured secret.
func verifyGitLabToken(header, secret string) bool {
	if header == "" || secret == "" {
		return false
	}
	return hmac.Equal([]byte(header), []byte(secret))
}

// verifyHMAC reports whether header is a valid "sha256=<hex>" HMAC-SHA256
// signature of body under secret, compared in constant time. An empty
// secret never verifies: callers must not invoke this with an unconfigured
// secret.
func verifyHMAC(header string, body []byte, secret string) bool {
	if header == "" || secret == "" {
		return false
	}
	parts := strings.SplitN(header, "=", 2)
	if len(parts) != 2 || parts[0] != "sha256" {
		return false
	}
	expectedSig, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), expectedSig)
}

// SignatureConfig holds the global webhook signature verification
// configuration. See verifyDelivery for how a delivery is accepted.
type SignatureConfig struct {
	// Secret is the global shared secret, used when the delivery's matched
	// project has none of its own.
	Secret string

	// Method determines which verification method to use.
	// "gitlab" = X-Gitlab-Token header comparison
	// "hmac"   = HMAC-SHA256 signature verification
	Method string
}

// verifyOne checks body/headers under method against a single secret. An
// empty secret never verifies.
func verifyOne(method string, h http.Header, body []byte, secret string) bool {
	if secret == "" {
		return false
	}
	switch method {
	case "hmac":
		return verifyHMAC(h.Get("X-Hub-Signature-256"), body, secret)
	default: // "gitlab", ""
		return verifyGitLabToken(h.Get("X-Gitlab-Token"), secret)
	}
}

// verifyDelivery reports whether a webhook delivery is authenticated: it
// verifies against projectSecret when non-empty, then against globalSecret
// when non-empty, and returns false when neither is set or neither
// verifies. See design.md D1/D2 - the project secret is tried first only so
// the common case short-circuits, not for security; either match accepts
// the delivery.
func verifyDelivery(method string, h http.Header, body []byte, projectSecret, globalSecret string) bool {
	if projectSecret != "" && verifyOne(method, h, body, projectSecret) {
		return true
	}
	if globalSecret != "" && verifyOne(method, h, body, globalSecret) {
		return true
	}
	return false
}

// webhookProbe reads just enough of a webhook body to route and authenticate
// the delivery before anything else in it is trusted - see design.md D1 and
// multi-source-projects D6: the project object's GitLab ID and repository
// URLs. A body that is not JSON yields an empty project (ID 0, no URLs), so
// only the global secret can accept it.
type webhookProbe struct {
	Project GitLabProject `json:"project"`
}

// probeWebhook extracts the payload's project object from body without
// trusting anything else in it.
func probeWebhook(body []byte) GitLabProject {
	var probe webhookProbe
	if err := json.Unmarshal(body, &probe); err != nil {
		return GitLabProject{}
	}
	return probe.Project
}
