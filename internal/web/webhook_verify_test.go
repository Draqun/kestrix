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
	"kestrix/internal/mocks"
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

func TestWebhookProvider(t *testing.T) {
	h := http.Header{}
	if p := webhookProvider(h); p != "gitlab" {
		t.Errorf("empty headers provider = %q, want gitlab", p)
	}
	h.Set("X-Gitlab-Token", "secret")
	if p := webhookProvider(h); p != "gitlab" {
		t.Errorf("gitlab token header provider = %q, want gitlab", p)
	}
	h.Set("X-GitHub-Event", "push")
	if p := webhookProvider(h); p != "github" {
		t.Errorf("github header provider = %q, want github", p)
	}
}

func TestVerifyOne(t *testing.T) {
	payload := []byte(`{"test":"data"}`)
	sig := computeHMAC(payload, "s")

	h := http.Header{}
	h.Set("X-Gitlab-Token", "s")
	if !verifyOne("gitlab", "gitlab", h, payload, "s") {
		t.Error("gitlab method should verify a matching token")
	}
	if !verifyOne("gitlab", "", h, payload, "s") {
		t.Error("empty method should default to gitlab token comparison")
	}

	hmacHeader := http.Header{}
	hmacHeader.Set("X-Hub-Signature-256", sig)
	if !verifyOne("gitlab", "hmac", hmacHeader, payload, "s") {
		t.Error("hmac method should verify a matching signature")
	}
	// A matching token under the wrong header name for the configured
	// method must not verify - see the "HMAC method is honoured" scenario.
	if verifyOne("gitlab", "hmac", h, payload, "s") {
		t.Error("hmac method must not accept a matching X-Gitlab-Token")
	}
	if verifyOne("gitlab", "gitlab", hmacHeader, payload, "s") {
		t.Error("gitlab method must not accept a matching X-Hub-Signature-256")
	}
	if verifyOne("gitlab", "gitlab", h, payload, "") {
		t.Error("an empty secret should never verify")
	}

	// GitHub provider always requires HMAC regardless of method
	ghHMAC := http.Header{}
	ghHMAC.Set("X-GitHub-Event", "push")
	ghHMAC.Set("X-Hub-Signature-256", sig)
	if !verifyOne("github", "gitlab", ghHMAC, payload, "s") {
		t.Error("github provider should verify matching HMAC even under gitlab method")
	}
	if !verifyOne("github", "hmac", ghHMAC, payload, "s") {
		t.Error("github provider should verify matching HMAC under hmac method")
	}
	ghToken := http.Header{}
	ghToken.Set("X-GitHub-Event", "push")
	ghToken.Set("X-Gitlab-Token", "s")
	if verifyOne("github", "gitlab", ghToken, payload, "s") {
		t.Error("github provider must reject token-only delivery under gitlab method")
	}
	if verifyOne("github", "hmac", ghToken, payload, "s") {
		t.Error("github provider must reject token-only delivery under hmac method")
	}
}

// TestVerifyDelivery pins design.md D1/D2: the project secret is tried
// first, the global secret is a fallback, and neither configured (or
// neither matching) rejects the delivery.
func TestVerifyDelivery(t *testing.T) {
	payload := []byte(`{"test":"data"}`)
	h := http.Header{}
	h.Set("X-Gitlab-Token", "project-secret")

	if !verifyDelivery("gitlab", "gitlab", h, payload, "project-secret", "global-secret") {
		t.Error("project secret match should verify")
	}

	global := http.Header{}
	global.Set("X-Gitlab-Token", "global-secret")
	if !verifyDelivery("gitlab", "gitlab", global, payload, "project-secret", "global-secret") {
		t.Error("global secret match should verify")
	}
	if !verifyDelivery("gitlab", "gitlab", global, payload, "", "global-secret") {
		t.Error("global secret match should verify when no project secret is set")
	}

	wrong := http.Header{}
	wrong.Set("X-Gitlab-Token", "wrong")
	if verifyDelivery("gitlab", "gitlab", wrong, payload, "project-secret", "") {
		t.Error("project set but wrong, and global absent, should not verify")
	}
	if verifyDelivery("gitlab", "gitlab", wrong, payload, "", "") {
		t.Error("both empty should not verify")
	}

	sig := computeHMAC(payload, "project-secret")
	hmacHeader := http.Header{}
	hmacHeader.Set("X-Hub-Signature-256", sig)
	if verifyDelivery("gitlab", "hmac", h, payload, "project-secret", "") {
		t.Error("hmac method should reject a matching X-Gitlab-Token")
	}
	tokenOnly := http.Header{}
	tokenOnly.Set("X-Gitlab-Token", "project-secret")
	if verifyDelivery("gitlab", "gitlab", hmacHeader, payload, "project-secret", "") {
		t.Error("gitlab method should reject a matching X-Hub-Signature-256")
	}

	// GitHub provider tests:
	// - Valid signature passes under gitlab and under hmac
	ghProjectHeader := http.Header{}
	ghProjectHeader.Set("X-GitHub-Event", "push")
	ghProjectHeader.Set("X-Hub-Signature-256", sig)
	if !verifyDelivery("github", "gitlab", ghProjectHeader, payload, "project-secret", "global-secret") {
		t.Error("github delivery with valid signature should verify under gitlab method")
	}
	if !verifyDelivery("github", "hmac", ghProjectHeader, payload, "project-secret", "global-secret") {
		t.Error("github delivery with valid signature should verify under hmac method")
	}

	// - Global secret fallback for GitHub
	sigGlobal := computeHMAC(payload, "global-secret")
	ghGlobalHeader := http.Header{}
	ghGlobalHeader.Set("X-GitHub-Event", "push")
	ghGlobalHeader.Set("X-Hub-Signature-256", sigGlobal)
	if !verifyDelivery("github", "gitlab", ghGlobalHeader, payload, "project-secret", "global-secret") {
		t.Error("github delivery with global secret should verify")
	}
	if !verifyDelivery("github", "gitlab", ghGlobalHeader, payload, "", "global-secret") {
		t.Error("github delivery with global secret should verify when project secret empty")
	}

	// - Only X-Gitlab-Token fails
	ghTokenHeader := http.Header{}
	ghTokenHeader.Set("X-GitHub-Event", "push")
	ghTokenHeader.Set("X-Gitlab-Token", "project-secret")
	if verifyDelivery("github", "gitlab", ghTokenHeader, payload, "project-secret", "global-secret") {
		t.Error("github delivery with only X-Gitlab-Token should fail under gitlab method")
	}
	if verifyDelivery("github", "hmac", ghTokenHeader, payload, "project-secret", "global-secret") {
		t.Error("github delivery with only X-Gitlab-Token should fail under hmac method")
	}

	// - Wrong secret or both empty fails for GitHub
	ghWrongHeader := http.Header{}
	ghWrongHeader.Set("X-GitHub-Event", "push")
	ghWrongHeader.Set("X-Hub-Signature-256", "sha256=badbadbad")
	if verifyDelivery("github", "gitlab", ghWrongHeader, payload, "project-secret", "") {
		t.Error("github wrong signature should fail")
	}
	if verifyDelivery("github", "gitlab", ghWrongHeader, payload, "", "") {
		t.Error("github both empty secrets should fail")
	}
}

func TestProbeWebhook(t *testing.T) {
	if p := probeWebhook("gitlab", []byte("not json")); p.GitLabProjectID != 0 || len(p.RepoURLs) != 0 {
		t.Errorf("non-JSON body (gitlab): probe = %+v, want empty", p)
	}
	if p := probeWebhook("gitlab", []byte(`{"ref":"refs/heads/main"}`)); p.GitLabProjectID != 0 {
		t.Errorf("body without an id (gitlab): id = %d, want 0", p.GitLabProjectID)
	}
	p := probeWebhook("gitlab", []byte(`{"project":{"id":42,"git_http_url":"https://h/a.git","git_ssh_url":"git@h:a.git"}}`))
	if p.GitLabProjectID != 42 {
		t.Errorf("id = %d, want 42", p.GitLabProjectID)
	}
	if urls := p.RepoURLs; len(urls) != 2 || urls[0] != "https://h/a.git" || urls[1] != "git@h:a.git" {
		t.Errorf("repo URLs = %v", urls)
	}

	// GitHub probe
	if p := probeWebhook("github", []byte("not json")); p.GitLabProjectID != 0 || len(p.RepoURLs) != 0 {
		t.Errorf("non-JSON body (github): probe = %+v, want empty", p)
	}
	if p := probeWebhook("github", []byte(`{"ref":"refs/heads/main"}`)); p.GitLabProjectID != 0 || len(p.RepoURLs) != 0 {
		t.Errorf("body without repository (github): probe = %+v, want empty", p)
	}
	ghBody := []byte(`{
		"repository": {
			"clone_url": "https://github.com/org/repo.git",
			"ssh_url": "git@github.com:org/repo.git",
			"html_url": "https://github.com/org/repo"
		}
	}`)
	pGH := probeWebhook("github", ghBody)
	if pGH.GitLabProjectID != 0 {
		t.Errorf("github probe should have GitLabProjectID 0, got %d", pGH.GitLabProjectID)
	}
	if len(pGH.RepoURLs) != 3 ||
		pGH.RepoURLs[0] != "https://github.com/org/repo.git" ||
		pGH.RepoURLs[1] != "git@github.com:org/repo.git" ||
		pGH.RepoURLs[2] != "https://github.com/org/repo" {
		t.Errorf("github repo URLs = %v", pGH.RepoURLs)
	}

	// GitHub probe with missing optional URLs
	ghBodyPartial := []byte(`{
		"repository": {
			"clone_url": "https://github.com/org/repo.git"
		}
	}`)
	pGHPartial := probeWebhook("github", ghBodyPartial)
	if len(pGHPartial.RepoURLs) != 1 || pGHPartial.RepoURLs[0] != "https://github.com/org/repo.git" {
		t.Errorf("partial github repo URLs = %v", pGHPartial.RepoURLs)
	}
}

func TestMatchProjectsProvider(t *testing.T) {
	proj := &domain.ProjectConfig{
		Name:    "my-gh-project",
		RepoURL: "https://github.com/org/repo.git",
		Sources: []domain.ProjectSource{
			{Name: "my-gh-project", URL: "https://github.com/org/repo.git", Primary: true},
		},
		GitLabProjectID: 42,
	}
	store := &mocks.MockProjectStore{
		Projects: map[string]*domain.ProjectConfig{
			"my-gh-project": proj,
		},
	}
	srv := NewServer(Config{})
	srv.projectStore = store

	// 1. A GitHub delivery carrying HTTPS clone_url matches
	pHTTPS := probeWebhook(providerGitHub, []byte(`{"repository":{"clone_url":"https://github.com/org/repo.git"}}`))
	keysHTTPS := sourceMatchKeys(pHTTPS.RepoURLs)
	matches, ignored, err := srv.matchProjects(providerGitHub, pHTTPS.GitLabProjectID, keysHTTPS)
	if err != nil || ignored || len(matches) != 1 || matches[0].cfg.Name != "my-gh-project" {
		t.Fatalf("github https match: matches=%v, ignored=%v, err=%v", matches, ignored, err)
	}

	// 2. A GitHub delivery carrying SSH URL matches the HTTPS registered project
	pSSH := probeWebhook(providerGitHub, []byte(`{"repository":{"ssh_url":"git@github.com:org/repo.git"}}`))
	keysSSH := sourceMatchKeys(pSSH.RepoURLs)
	matchesSSH, ignoredSSH, err := srv.matchProjects(providerGitHub, pSSH.GitLabProjectID, keysSSH)
	if err != nil || ignoredSSH || len(matchesSSH) != 1 || matchesSSH[0].cfg.Name != "my-gh-project" {
		t.Fatalf("github ssh match: matches=%v, ignored=%v, err=%v", matchesSSH, ignoredSSH, err)
	}

	// 3. GitLab-ID fallback applies only to GitLab
	// If a probe has gitlabProjectID = 42 and no matching keys:
	matchesGitLab, ignoredGitLab, err := srv.matchProjects(providerGitLab, 42, nil)
	if err != nil || ignoredGitLab || len(matchesGitLab) != 1 || matchesGitLab[0].cfg.Name != "my-gh-project" {
		t.Fatalf("gitlab fallback match: matches=%v, ignored=%v, err=%v", matchesGitLab, ignoredGitLab, err)
	}
	// For GitHub, the same gitlabProjectID does not match
	matchesGHFallback, _, err := srv.matchProjects(providerGitHub, 42, nil)
	if err != nil || len(matchesGHFallback) != 0 {
		t.Fatalf("github should not use gitlab ID fallback: matches=%v, err=%v", matchesGHFallback, err)
	}
}
