package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

const (
	providerGitLab = "gitlab"
	providerGitHub = "github"
)

// webhookProvider returns providerGitHub if the X-GitHub-Event header is present,
// otherwise providerGitLab. See design D1.
func webhookProvider(h http.Header) string {
	if h.Get("X-GitHub-Event") != "" {
		return providerGitHub
	}
	return providerGitLab
}

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

// verifyOne checks body/headers under provider and method against a single secret. An
// empty secret never verifies.
func verifyOne(provider, method string, h http.Header, body []byte, secret string) bool {
	if secret == "" {
		return false
	}
	if provider == providerGitHub {
		return verifyHMAC(h.Get("X-Hub-Signature-256"), body, secret)
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
// verifies. See design.md D1/D2 - GitHub deliveries always verify HMAC,
// while GitLab deliveries use the configured method.
func verifyDelivery(provider, method string, h http.Header, body []byte, projectSecret, globalSecret string) bool {
	if projectSecret != "" && verifyOne(provider, method, h, body, projectSecret) {
		return true
	}
	if globalSecret != "" && verifyOne(provider, method, h, body, globalSecret) {
		return true
	}
	return false
}

// webhookProbe reads just enough of a webhook body to route and authenticate
// the delivery before anything else in it is trusted - see design.md D1, D3 and
// multi-source-projects D6: repository URLs, plus the GitLab ID when the provider is GitLab.
type webhookProbe struct {
	GitLabProjectID int
	RepoURLs        []string
}

// probeWebhook extracts the payload's match data from body without
// trusting anything else in it.
func probeWebhook(provider string, body []byte) webhookProbe {
	if provider == providerGitHub {
		var raw struct {
			Repository struct {
				CloneURL string `json:"clone_url"`
				SSHURL   string `json:"ssh_url"`
				HTMLURL  string `json:"html_url"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(body, &raw); err != nil {
			return webhookProbe{}
		}
		var urls []string
		for _, u := range []string{raw.Repository.CloneURL, raw.Repository.SSHURL, raw.Repository.HTMLURL} {
			if u = strings.TrimSpace(u); u != "" {
				urls = append(urls, u)
			}
		}
		return webhookProbe{
			RepoURLs: urls,
		}
	}

	var raw struct {
		Project GitLabProject `json:"project"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return webhookProbe{}
	}
	return webhookProbe{
		GitLabProjectID: raw.Project.ID,
		RepoURLs:        collectRepoURLs(raw.Project),
	}
}
