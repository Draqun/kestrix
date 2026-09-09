package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/service"
)

// multiSourcePipeline declares a subpath repo "lib" and a remote repo "shared"
// so a secondary push can be told apart from a primary one.
const multiSourcePipeline = `project "%s" {
	ci_repo "lib" {
		path = "lib"
	}
	ci_repo "shared" {
		url = "https://git.example.test/team/shared.git"
		ref = "main"
	}
	ci_task "build_lib" {
		trigger = "repo.lib.changed"
		execute {
			repo   = "lib"
			script = ":"
		}
	}
	ci_task "sync_shared" {
		trigger = "repo.shared.changed"
		execute {
			repo   = "shared"
			script = ":"
		}
	}
}
`

const sharedRepoURL = "https://git.example.test/team/shared.git"

const zzRepoURL = "https://git.example.test/team/zz.git"

// unsortedSourcePipeline declares its remote ci_repo blocks in an order that is
// not the one the store returns them in (zz_last before shared), so a test can
// tell whether the derived set is canonicalized before being compared with the
// stored one.
const unsortedSourcePipeline = `project "%s" {
	ci_repo "zz_last" {
		url = "https://git.example.test/team/zz.git"
		ref = "main"
	}
	ci_repo "shared" {
		url = "https://git.example.test/team/shared.git"
		ref = "main"
	}
	ci_task "sync_shared" {
		trigger = "repo.shared.changed"
		execute {
			repo   = "shared"
			script = ":"
		}
	}
}
`

// registeredProject writes pipeline into its own directory and returns a
// project whose primary source is that directory and whose secondary source
// is the shared repository, as the service would have derived them.
func registeredProject(t *testing.T, name, pipeline string, gitlabID int) *domain.ProjectConfig {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pipeline.tf"), []byte(strings.ReplaceAll(pipeline, "%s", name)), 0o600); err != nil {
		t.Fatal(err)
	}
	url := "file://" + dir
	return &domain.ProjectConfig{
		Name: name, RepoURL: url, RepoRef: "main", PipelineDir: ".", GitLabProjectID: gitlabID,
		Sources: []domain.ProjectSource{
			{Name: name, URL: url, Primary: true},
			{Name: "shared", URL: sharedRepoURL},
		},
	}
}

// sourcesServer is a history-recording server whose store holds projects.
func sourcesServer(t *testing.T, projects ...*domain.ProjectConfig) (*Server, *mocks.MockProjectStore, *mocks.MockWebhookLogSink, *testSourceSessions) {
	t.Helper()
	srv, log, _ := historyServer(t)
	store := &mocks.MockProjectStore{Projects: map[string]*domain.ProjectConfig{}}
	for _, p := range projects {
		store.Projects[p.Name] = p
	}
	sessions := &testSourceSessions{}
	srv.projectStore = store
	srv.sourceSessions = sessions
	return srv, store, log, sessions
}

func sourcePush(projectID int, urls []string, sha string, files ...string) string {
	quoted := make([]string, 0, len(files))
	for _, f := range files {
		quoted = append(quoted, `"`+f+`"`)
	}
	project := map[string]interface{}{"id": projectID, "path_with_namespace": "team/pushed", "default_branch": "main"}
	if len(urls) > 0 {
		project["git_http_url"] = urls[0]
	}
	if len(urls) > 1 {
		project["git_ssh_url"] = urls[1]
	}
	projectJSON, _ := json.Marshal(project)
	return `{"object_kind":"push","ref":"refs/heads/main","checkout_sha":"` + sha + `","project":` + string(projectJSON) +
		`,"commits":[{"id":"` + sha + `","added":[],"modified":[` + strings.Join(quoted, ",") + `],"removed":[]}]}`
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return resp
}

func resultsOf(t *testing.T, resp map[string]interface{}) []map[string]interface{} {
	t.Helper()
	raw, ok := resp["results"].([]interface{})
	if !ok {
		t.Fatalf("response has no results list: %v", resp)
	}
	out := make([]map[string]interface{}, 0, len(raw))
	for _, r := range raw {
		out = append(out, r.(map[string]interface{}))
	}
	return out
}

func stringsOf(v interface{}) []string {
	items, _ := v.([]interface{})
	out := make([]string, 0, len(items))
	for _, i := range items {
		out = append(out, i.(string))
	}
	return out
}

// A push to a repository that is a secondary source resolves the project,
// marks exactly that ci_repo changed, loads the pipeline at the configured
// ref rather than the event SHA, and attributes the history row to the source.
func TestWebhookSecondarySourceResolvesProject(t *testing.T) {
	srv, store, log, sessions := sourcesServer(t, registeredProject(t, "checkout", multiSourcePipeline, 0))

	rec := postWebhook(t, srv, sourcePush(999, []string{"git@git.example.test:team/shared.git"}, "def456", "lib/util.go"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	resp := decodeBody(t, rec)
	if resp["status"] != "ok" || resp["project"] != "checkout" {
		t.Fatalf("response = %v", resp)
	}
	// The secondary repo changed as a whole; the primary's subpath "lib" is
	// not matched even though the pushed file lives under "lib/".
	if got := stringsOf(resp["changed"]); len(got) != 1 || got[0] != "shared" {
		t.Errorf("changed = %v, want [shared]", got)
	}
	if got := stringsOf(resp["tasks"]); len(got) != 1 || got[0] != "sync_shared" {
		t.Errorf("tasks = %v, want [sync_shared]", got)
	}
	if results := resultsOf(t, resp); len(results) != 1 || results[0]["source_name"] != "shared" {
		t.Errorf("results = %v", results)
	}
	if len(sessions.requests) != 1 || sessions.requests[0].Revision != "" || !sessions.requests[0].Primary || sessions.requests[0].Purpose != "webhook" {
		t.Errorf("primary source requests = %+v, want one at the configured ref, not def456", sessions.requests)
	}
	events := log.List()
	if len(events) != 1 || events[0].ProjectName != "checkout" || events[0].SourceName != "shared" ||
		events[0].Revision != "def456" || events[0].Status != domain.WebhookStatusTriggered {
		t.Errorf("history = %+v", events)
	}
	// A secondary load reads the primary pipeline at repo_ref, the canonical
	// definition, so it re-derives the sources; they match what is stored, so
	// nothing is written.
	if len(store.ReplaceSourcesCalls) != 0 {
		t.Errorf("unchanged sources were rewritten: %+v", store.ReplaceSourcesCalls)
	}
}

// A secondary delivery whose canonical definition declares more than is
// recorded refreshes the stored set. The delivery still has to match, so the
// stored set is outdated in a source other than the pushed one.
func TestWebhookSecondaryPushRefreshesOutdatedSources(t *testing.T) {
	project := registeredProject(t, "checkout", unsortedSourcePipeline, 0)
	project.Sources = []domain.ProjectSource{
		{Name: "checkout", URL: project.RepoURL, Primary: true},
		{Name: "shared", URL: sharedRepoURL},
	}
	srv, store, _, _ := sourcesServer(t, project)

	resp := decodeBody(t, postWebhook(t, srv, sourcePush(0, []string{sharedRepoURL}, "def456", "lib/util.go")))

	if resp["status"] != "ok" {
		t.Fatalf("response = %v", resp)
	}
	if len(store.ReplaceSourcesCalls) != 1 || store.ReplaceSourcesCalls[0].Name != "checkout" {
		t.Fatalf("ReplaceSources calls = %+v", store.ReplaceSourcesCalls)
	}
	if got := len(store.ReplaceSourcesCalls[0].Sources); got != 3 {
		t.Fatalf("recorded %d sources, want 3 (primary, shared, zz_last)", got)
	}
	if len(project.Sources) != 3 {
		t.Errorf("in-memory project sources not updated: %+v", project.Sources)
	}
}

// A primary delivery never rewrites the source set, whatever branch it targets.
// It loads its own immutable SHA, which need not be the head of repo_ref:
// deliveries can arrive out of order or be retried, and deriving routing from
// an older commit would resurrect a ci_repo a later commit removed. Here the
// stored set deliberately disagrees with the pipeline, and must survive.
func TestWebhookPrimaryPushNeverRewritesSources(t *testing.T) {
	project := registeredProject(t, "checkout", multiSourcePipeline, 0)
	stored := []domain.ProjectSource{{Name: "checkout", URL: project.RepoURL, Primary: true}}
	project.Sources = stored
	srv, store, _, _ := sourcesServer(t, project)

	// "main" is the project's repo_ref, the branch that used to trigger a
	// refresh; "feature" never did.
	for _, branch := range []string{"main", "feature"} {
		payload := strings.Replace(
			sourcePush(0, []string{project.RepoURL}, "abc123", "lib/util.go"),
			"refs/heads/main", "refs/heads/"+branch, 1)
		if resp := decodeBody(t, postWebhook(t, srv, payload)); resp["status"] != "ok" {
			t.Fatalf("branch %s: response = %v", branch, resp)
		}
	}

	if len(store.ReplaceSourcesCalls) != 0 {
		t.Fatalf("primary delivery rewrote sources: %+v", store.ReplaceSourcesCalls)
	}
	if len(project.Sources) != 1 || project.Sources[0].Name != "checkout" {
		t.Errorf("stored sources changed: %+v", project.Sources)
	}
}

// A ci_repo renamed in Git since the sources were last derived is still
// marked changed under the name the parsed DAG uses, not the stale stored one.
func TestWebhookSecondaryChangedUsesParsedRepoName(t *testing.T) {
	renamed := strings.ReplaceAll(strings.ReplaceAll(multiSourcePipeline, `ci_repo "shared"`, `ci_repo "libshared"`), "repo.shared.changed", "repo.libshared.changed")
	renamed = strings.ReplaceAll(renamed, `repo   = "shared"`, `repo   = "libshared"`)
	srv, _, log, _ := sourcesServer(t, registeredProject(t, "checkout", renamed, 0))

	resp := decodeBody(t, postWebhook(t, srv, sourcePush(0, []string{sharedRepoURL}, "def456", "lib/util.go")))

	if got := stringsOf(resp["changed"]); len(got) != 1 || got[0] != "libshared" {
		t.Errorf("changed = %v, want [libshared]", got)
	}
	if got := stringsOf(resp["tasks"]); len(got) != 1 || got[0] != "sync_shared" {
		t.Errorf("tasks = %v, want [sync_shared]", got)
	}
	// The attributed source name agrees with the changed set.
	if results := resultsOf(t, resp); results[0]["source_name"] != "libshared" {
		t.Errorf("results = %v, want source_name libshared", results)
	}
	if events := log.List(); len(events) != 1 || events[0].SourceName != "libshared" {
		t.Errorf("history = %+v, want source_name libshared", events)
	}
}

// A primary push to a branch other than repo_ref is evaluated against that
// branch's pipeline but must not rewrite the project's routing.
func TestWebhookPrimaryPushToOtherBranchDoesNotRefreshSources(t *testing.T) {
	project := registeredProject(t, "checkout", multiSourcePipeline, 0)
	srv, store, _, sessions := sourcesServer(t, project)

	payload := strings.Replace(sourcePush(0, []string{project.RepoURL}, "abc123", "lib/util.go"), "refs/heads/main", "refs/heads/feature", 1)
	resp := decodeBody(t, postWebhook(t, srv, payload))

	if resp["status"] != "ok" || len(stringsOf(resp["changed"])) != 2 {
		t.Fatalf("response = %v", resp)
	}
	if len(sessions.requests) != 1 || sessions.requests[0].Revision != "abc123" {
		t.Errorf("requests = %+v, want the branch's event SHA", sessions.requests)
	}
	if len(store.ReplaceSourcesCalls) != 0 {
		t.Errorf("feature-branch push rewrote sources: %+v", store.ReplaceSourcesCalls)
	}
}

// The ignored path logs match keys, never the raw payload URLs, so a URL that
// smuggles a token stays out of the server log as well as the history.
func TestWebhookIgnoredLogDoesNotLeakCredentials(t *testing.T) {
	srv, _, _, _ := sourcesServer(t, registeredProject(t, "checkout", multiSourcePipeline, 0))
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	rec := postWebhook(t, srv, sourcePush(4242, []string{"https://user:glpat-secret@git.example.test/team/other.git"}, "abc123", "src/main.go"))

	if resp := decodeBody(t, rec); resp["status"] != "ignored" {
		t.Fatalf("response = %v", resp)
	}
	if strings.Contains(buf.String(), "glpat-secret") {
		t.Errorf("server log leaked the credential: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "unknown project") {
		t.Errorf("ignored delivery was not logged: %s", buf.String())
	}
}

// When every match fails, a pipeline load failure (422) outranks an unusable
// push (400) regardless of project-name order.
func TestWebhookAllMatchesFailingPrefersUnprocessable(t *testing.T) {
	alpha := registeredProject(t, "alpha", multiSourcePipeline, 0)
	beta := registeredProject(t, "beta", `project "beta" {`, 0)
	beta.Sources = []domain.ProjectSource{beta.Sources[0], {Name: "alpha-lib", URL: alpha.RepoURL}}
	srv, _, log, _ := sourcesServer(t, alpha, beta)

	// No checkout SHA: alpha (primary) fails with 400, beta (secondary, broken pipeline) with 422.
	rec := postWebhook(t, srv, sourcePush(0, []string{alpha.RepoURL}, "", "lib/util.go"))

	resp := decodeBody(t, rec)
	if rec.Code != http.StatusUnprocessableEntity || resp["status"] != "error" {
		t.Fatalf("status = %d response = %v", rec.Code, resp)
	}
	results := resultsOf(t, resp)
	if len(results) != 2 || results[0]["project"] != "alpha" || results[1]["project"] != "beta" ||
		!strings.Contains(results[0]["error"].(string), "checkout SHA") || !strings.Contains(results[1]["error"].(string), "parse pipeline") {
		t.Fatalf("results = %v", results)
	}
	if events := log.List(); len(events) != 2 {
		t.Errorf("history = %+v, want one row per failed project", events)
	}
}

func TestWebhookSecondaryPushWithOnlyExcludedFilesIsSkipped(t *testing.T) {
	srv, _, log, _ := sourcesServer(t, registeredProject(t, "checkout", multiSourcePipeline, 0))

	rec := postWebhook(t, srv, sourcePush(0, []string{sharedRepoURL}, "def456", "README.md"))

	resp := decodeBody(t, rec)
	if rec.Code != http.StatusOK || resp["status"] != "ok" || len(stringsOf(resp["changed"])) != 0 {
		t.Fatalf("status = %d response = %v", rec.Code, resp)
	}
	events := log.List()
	if len(events) != 1 || events[0].Status != domain.WebhookStatusSkipped || events[0].SourceName != "shared" {
		t.Errorf("history = %+v", events)
	}
}

// A primary push keeps the pre-fan-out behaviour: event SHA checkout and path
// matching against every ci_repo. It does not rewrite the source set - see
// TestWebhookPrimaryPushNeverRewritesSources.
func TestWebhookPrimaryPushMatchesByURLWithoutRewritingSources(t *testing.T) {
	project := registeredProject(t, "checkout", multiSourcePipeline, 0)
	project.Sources = project.Sources[:1] // shared not yet derived
	srv, store, log, sessions := sourcesServer(t, project)

	rec := postWebhook(t, srv, sourcePush(0, []string{project.RepoURL}, "abc123", "lib/util.go"))

	resp := decodeBody(t, rec)
	if rec.Code != http.StatusOK || resp["status"] != "ok" {
		t.Fatalf("status = %d response = %v", rec.Code, resp)
	}
	if got := stringsOf(resp["changed"]); len(got) != 2 || got[0] != "lib" || got[1] != "shared" {
		t.Errorf("changed = %v, want [lib shared]", got)
	}
	if len(sessions.requests) != 1 || sessions.requests[0].Revision != "abc123" {
		t.Errorf("requests = %+v, want the event SHA", sessions.requests)
	}
	if len(store.ReplaceSourcesCalls) != 0 {
		t.Errorf("primary delivery rewrote sources: %+v", store.ReplaceSourcesCalls)
	}
	if events := log.List(); len(events) != 1 || events[0].SourceName != "checkout" {
		t.Errorf("history = %+v, want the primary attributed to the project name", events)
	}
}

// The GitLab project ID keeps resolving a registration whose URL matched no
// source, and is treated as the primary source.
func TestWebhookFallsBackToGitLabProjectID(t *testing.T) {
	project := registeredProject(t, "legacy", pipelineWithTaskTF, 4242)
	project.Sources = nil
	srv, _, log, sessions := sourcesServer(t, project)

	rec := postWebhook(t, srv, sourcePush(4242, []string{"https://git.example.test/team/unrelated.git"}, "abc123", "src/main.go"))

	resp := decodeBody(t, rec)
	// The result names the registration, not the HCL project ("demo").
	if rec.Code != http.StatusOK || resp["status"] != "ok" || resp["project"] != "legacy" {
		t.Fatalf("status = %d response = %v", rec.Code, resp)
	}
	if len(sessions.requests) != 1 || sessions.requests[0].Revision != "abc123" {
		t.Errorf("requests = %+v", sessions.requests)
	}
	if events := log.List(); len(events) != 1 || events[0].ProjectName != "legacy" || events[0].SourceName != "legacy" {
		t.Errorf("history = %+v", events)
	}
}

// A URL match wins over a conflicting project ID: the project matched by ID
// is not resolved as well.
func TestWebhookURLMatchTakesPrecedenceOverProjectID(t *testing.T) {
	byURL := registeredProject(t, "checkout", multiSourcePipeline, 0)
	byID := registeredProject(t, "other", pipelineWithTaskTF, 4242)
	byID.Sources = byID.Sources[:1]
	srv, _, log, _ := sourcesServer(t, byURL, byID)

	rec := postWebhook(t, srv, sourcePush(4242, []string{sharedRepoURL}, "def456", "lib/util.go"))

	resp := decodeBody(t, rec)
	if resp["status"] != "ok" || resp["project"] != "checkout" {
		t.Fatalf("response = %v", resp)
	}
	events := log.List()
	if len(events) != 1 || events[0].ProjectName != "checkout" {
		t.Errorf("history = %+v, want only checkout", events)
	}
}

func TestWebhookNothingMatchesIsIgnored(t *testing.T) {
	srv, _, log, _ := sourcesServer(t, registeredProject(t, "checkout", multiSourcePipeline, 0))

	rec := postWebhook(t, srv, sourcePush(4242, []string{"https://git.example.test/team/unrelated.git"}, "abc123", "src/main.go"))

	resp := decodeBody(t, rec)
	if rec.Code != http.StatusOK || resp["status"] != "ignored" || resp["reason"] != "unknown project" {
		t.Fatalf("status = %d response = %v", rec.Code, resp)
	}
	if events := log.List(); len(events) != 1 || events[0].Status != domain.WebhookStatusIgnored || events[0].ProjectName != "" {
		t.Errorf("history = %+v", events)
	}
}

// A payload URL with embedded credentials is not matched, and never reaches
// the store or the history row; the delivery is routed by the remaining URL.
func TestWebhookDropsURLsWithCredentials(t *testing.T) {
	srv, _, log, _ := sourcesServer(t, registeredProject(t, "checkout", multiSourcePipeline, 0))

	rec := postWebhook(t, srv, sourcePush(0, []string{"https://user:token@git.example.test/team/shared.git", sharedRepoURL}, "def456", "lib/util.go"))

	if resp := decodeBody(t, rec); resp["status"] != "ok" || resp["project"] != "checkout" {
		t.Fatalf("response = %v", resp)
	}
	for _, e := range log.List() {
		if strings.Contains(e.Repo+e.Error+e.SourceName, "token") {
			t.Errorf("credential leaked into history: %+v", e)
		}
	}
}

// A store failure while resolving the matching projects happens before
// verification, so it fails closed: 503 without disclosing the error and
// without a history row, as the ID-lookup path already does.
func TestWebhookSourceLookupFailureFailsClosed(t *testing.T) {
	srv, store, log, _ := sourcesServer(t, registeredProject(t, "checkout", multiSourcePipeline, 0))
	store.FindBySourceKeyErr = errors.New("database unavailable")

	rec := postWebhook(t, srv, sourcePush(0, []string{sharedRepoURL}, "def456", "lib/util.go"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if resp := decodeBody(t, rec); resp["error"] != "service unavailable" {
		t.Errorf("response = %v", resp)
	}
	if events := log.List(); len(events) != 0 {
		t.Errorf("history = %+v, want nothing recorded", events)
	}
}

// A definition whose ci_repo blocks are not declared alphabetically still
// counts as unchanged: refreshSources compares the derived set with the stored
// one, and both are in the store's canonical order. Before DeriveSources
// sorted, this wrote project_sources on every single delivery.
func TestWebhookSecondaryPushSkipsWriteRegardlessOfDeclarationOrder(t *testing.T) {
	project := registeredProject(t, "checkout", unsortedSourcePipeline, 0)
	// What the store returns for that pipeline: primary first, then by name.
	project.Sources = []domain.ProjectSource{
		{Name: "checkout", URL: project.RepoURL, Primary: true},
		{Name: "shared", URL: sharedRepoURL},
		{Name: "zz_last", URL: zzRepoURL},
	}
	srv, store, _, _ := sourcesServer(t, project)

	resp := decodeBody(t, postWebhook(t, srv, sourcePush(0, []string{sharedRepoURL}, "def456", "lib/util.go")))

	if resp["status"] != "ok" {
		t.Fatalf("response = %v", resp)
	}
	if len(store.ReplaceSourcesCalls) != 0 {
		t.Errorf("unchanged definition still wrote sources: %+v", store.ReplaceSourcesCalls)
	}
}

// postWebhookWithToken sends a push payload presenting token (or no header
// when token is empty) instead of the historyServer global secret.
func postWebhookWithToken(t *testing.T, srv *Server, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
	if token != "" {
		req.Header.Set("X-Gitlab-Token", token)
	}
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)
	return rec
}

// Each matched project verifies the delivery against its own secret: the one
// whose secret does not match is skipped without a history row, the other
// proceeds, and the response lists only the processed project.
func TestWebhookFanOutVerifiesEachProjectSeparately(t *testing.T) {
	checkout := registeredProject(t, "checkout", multiSourcePipeline, 0)
	checkout.WebhookSecret = "checkout-secret"
	billing := registeredProject(t, "billing", multiSourcePipeline, 0)
	billing.WebhookSecret = "billing-secret"
	srv, _, log, _ := sourcesServer(t, checkout, billing)
	srv.cfg.Signature.Secret = ""

	rec := postWebhookWithToken(t, srv, sourcePush(0, []string{sharedRepoURL}, "def456", "lib/util.go"), "checkout-secret")

	resp := decodeBody(t, rec)
	if rec.Code != http.StatusOK || resp["status"] != "ok" || resp["project"] != "checkout" {
		t.Fatalf("status = %d response = %v", rec.Code, resp)
	}
	if results := resultsOf(t, resp); len(results) != 1 || results[0]["project"] != "checkout" {
		t.Errorf("results = %v, want only checkout", results)
	}
	events := log.List()
	if len(events) != 1 || events[0].ProjectName != "checkout" {
		t.Errorf("history = %+v, want a row for checkout only", events)
	}
}

// A delivery that no matched project's secret accepts, with no global secret,
// is rejected before anything is parsed or recorded.
func TestWebhookNoMatchedProjectVerifyingAnswers401(t *testing.T) {
	checkout := registeredProject(t, "checkout", multiSourcePipeline, 0)
	checkout.WebhookSecret = "checkout-secret"
	srv, _, log, sessions := sourcesServer(t, checkout)
	srv.cfg.Signature.Secret = ""

	rec := postWebhookWithToken(t, srv, sourcePush(0, []string{sharedRepoURL}, "def456", "lib/util.go"), "wrong-secret")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if len(log.List()) != 0 || len(sessions.requests) != 0 {
		t.Errorf("unverified delivery was processed: history=%+v requests=%+v", log.List(), sessions.requests)
	}
}

// When nothing matches, only the global secret can accept the delivery: with
// it the delivery is recorded as ignored, without it the answer is 401.
func TestWebhookUnmatchedDeliveryNeedsGlobalSecret(t *testing.T) {
	payload := sourcePush(4242, []string{"https://git.example.test/team/unrelated.git"}, "abc123", "src/main.go")

	srv, _, log, _ := sourcesServer(t, registeredProject(t, "checkout", multiSourcePipeline, 0))
	if rec := postWebhookWithToken(t, srv, payload, historyWebhookSecret); rec.Code != http.StatusOK || decodeBody(t, rec)["status"] != "ignored" {
		t.Fatalf("with global secret: status = %d body = %s", rec.Code, rec.Body.String())
	}
	if events := log.List(); len(events) != 1 || events[0].Status != domain.WebhookStatusIgnored {
		t.Errorf("history = %+v", events)
	}

	srv, _, log, _ = sourcesServer(t, registeredProject(t, "checkout", multiSourcePipeline, 0))
	srv.cfg.Signature.Secret = ""
	if rec := postWebhookWithToken(t, srv, payload, "anything"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without global secret: status = %d body = %s", rec.Code, rec.Body.String())
	}
	if len(log.List()) != 0 {
		t.Errorf("unverified ignored delivery was recorded: %+v", log.List())
	}
}

// primaryOnlyStore returns a project from the key lookup without any Sources,
// as a store from before the sources table would; the match is then treated
// as the primary source.
type primaryOnlyStore struct {
	*mocks.MockProjectStore
	project *domain.ProjectConfig
}

func (s *primaryOnlyStore) FindBySourceKey([]string) ([]*domain.ProjectConfig, error) {
	return []*domain.ProjectConfig{s.project}, nil
}

func TestWebhookMatchWithoutSourcesIsTreatedAsPrimary(t *testing.T) {
	project := registeredProject(t, "checkout", multiSourcePipeline, 0)
	project.Sources = nil
	srv, store, log, sessions := sourcesServer(t, project)
	srv.projectStore = &primaryOnlyStore{MockProjectStore: store, project: project}

	rec := postWebhook(t, srv, sourcePush(0, []string{project.RepoURL}, "abc123", "lib/util.go"))

	if resp := decodeBody(t, rec); resp["status"] != "ok" {
		t.Fatalf("response = %v", resp)
	}
	if len(sessions.requests) != 1 || sessions.requests[0].Revision != "abc123" {
		t.Errorf("requests = %+v, want a primary load at the event SHA", sessions.requests)
	}
	if events := log.List(); len(events) != 1 || events[0].SourceName != "checkout" {
		t.Errorf("history = %+v", events)
	}
}

// One repository declared by two projects fans out: each project is resolved
// against its own pipeline, recorded separately, and listed in name order.
func TestWebhookFansOutToEveryMatchingProject(t *testing.T) {
	srv, _, log, _ := sourcesServer(t,
		registeredProject(t, "checkout", multiSourcePipeline, 0),
		registeredProject(t, "billing", multiSourcePipeline, 0),
	)

	rec := postWebhook(t, srv, sourcePush(0, []string{sharedRepoURL}, "def456", "lib/util.go"))

	resp := decodeBody(t, rec)
	if rec.Code != http.StatusOK || resp["status"] != "ok" {
		t.Fatalf("status = %d response = %v", rec.Code, resp)
	}
	if _, ok := resp["project"]; ok {
		t.Errorf("multi-match response carries a single top-level project: %v", resp)
	}
	results := resultsOf(t, resp)
	if len(results) != 2 || results[0]["project"] != "billing" || results[1]["project"] != "checkout" {
		t.Fatalf("results = %v, want billing then checkout", results)
	}
	for _, r := range results {
		if r["source_name"] != "shared" || r["status"] != "ok" || len(stringsOf(r["tasks"])) != 1 {
			t.Errorf("result = %v", r)
		}
	}
	events := log.List()
	if len(events) != 2 || events[0].ProjectName != "billing" || events[1].ProjectName != "checkout" {
		t.Errorf("history = %+v, want one row per project", events)
	}
}

// A failure in one matched project is that project's error entry and history
// row; the other project is still resolved and the delivery answers 200.
func TestWebhookIsolatesFailuresAcrossMatches(t *testing.T) {
	broken := registeredProject(t, "billing", `project "billing" {`, 0)
	srv, _, log, _ := sourcesServer(t, registeredProject(t, "checkout", multiSourcePipeline, 0), broken)

	rec := postWebhook(t, srv, sourcePush(0, []string{sharedRepoURL}, "def456", "lib/util.go"))

	resp := decodeBody(t, rec)
	if rec.Code != http.StatusOK || resp["status"] != "ok" {
		t.Fatalf("status = %d response = %v", rec.Code, resp)
	}
	results := resultsOf(t, resp)
	if len(results) != 2 || results[0]["status"] != "error" || results[0]["error"] == "" || results[1]["status"] != "ok" {
		t.Fatalf("results = %v", results)
	}
	events := log.List()
	if len(events) != 2 || events[0].ProjectName != "billing" || events[0].Status != domain.WebhookStatusError ||
		events[1].ProjectName != "checkout" || events[1].Status != domain.WebhookStatusTriggered {
		t.Errorf("history = %+v", events)
	}
}

// When every matched project fails the delivery reports error, with the
// single-match document still carrying the message and the pre-fan-out code.
func TestWebhookAllMatchesFailingReportsError(t *testing.T) {
	srv, _, log, _ := sourcesServer(t, registeredProject(t, "billing", `project "billing" {`, 0))

	rec := postWebhook(t, srv, sourcePush(0, []string{sharedRepoURL}, "def456", "lib/util.go"))

	resp := decodeBody(t, rec)
	if rec.Code != http.StatusUnprocessableEntity || resp["status"] != "error" || resp["project"] != "billing" {
		t.Fatalf("status = %d response = %v", rec.Code, resp)
	}
	if msg, _ := resp["error"].(string); !strings.Contains(msg, "parse pipeline directory") {
		t.Errorf("error = %v", resp["error"])
	}
	if events := log.List(); len(events) != 1 || events[0].Status != domain.WebhookStatusError {
		t.Errorf("history = %+v", events)
	}
}

// The single-match document keeps every key the pre-fan-out response had,
// with the same values, alongside the new results list.
func TestWebhookSingleMatchResponseKeepsLegacyKeys(t *testing.T) {
	project := registeredProject(t, "checkout", multiSourcePipeline, 0)
	srv, _, _, _ := sourcesServer(t, project)

	rec := postWebhook(t, srv, sourcePush(0, []string{project.RepoURL}, "abc123", "lib/util.go"))

	resp := decodeBody(t, rec)
	legacy := map[string]interface{}{
		"status": "ok", "project": "checkout", "count": float64(2), "dispatched": false,
	}
	for key, want := range legacy {
		if resp[key] != want {
			t.Errorf("%s = %v, want %v", key, resp[key], want)
		}
	}
	if got := stringsOf(resp["changed"]); len(got) != 2 {
		t.Errorf("changed = %v", got)
	}
	if got := stringsOf(resp["tasks"]); len(got) != 2 || got[0] != "build_lib" || got[1] != "sync_shared" {
		t.Errorf("tasks = %v", got)
	}
	if _, ok := resp["pipeline_url"]; ok {
		t.Errorf("pipeline_url present without a dispatch attempt: %v", resp)
	}
	if results := resultsOf(t, resp); len(results) != 1 || results[0]["project"] != "checkout" {
		t.Errorf("results = %v", results)
	}
}

// gitlabVariableCapture is a fake GitLab that records the variables set on
// each pipeline it creates.
type gitlabVariableCapture struct {
	mu   sync.Mutex
	vars []map[string]string
}

func (g *gitlabVariableCapture) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pipeline"):
			g.mu.Lock()
			g.vars = append(g.vars, map[string]string{})
			g.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{"id": len(g.vars), "web_url": "https://gitlab.example/p/-/pipelines/1"})
		case strings.HasSuffix(r.URL.Path, "/variables"):
			var v map[string]string
			json.NewDecoder(r.Body).Decode(&v)
			g.mu.Lock()
			g.vars[len(g.vars)-1][v["key"]] = v["value"]
			g.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte("{}"))
		default:
			http.NotFound(w, r)
		}
	}))
}

// Dispatch tells the GitLab job which source fired and at which revision, for
// a primary and for a secondary delivery.
func TestWebhookDispatchCarriesSourceVariables(t *testing.T) {
	capture := &gitlabVariableCapture{}
	gitlab := capture.server()
	defer gitlab.Close()
	project := registeredProject(t, "checkout", multiSourcePipeline, 77)
	srv, _, _, _ := sourcesServer(t, project)
	srv.cfg.Dispatch = DispatchConfig{Enabled: true, GitLabURL: gitlab.URL, GitLabToken: "glpat-x"}

	primary := decodeBody(t, postWebhook(t, srv, sourcePush(0, []string{project.RepoURL}, "abc123", "lib/util.go")))
	secondary := decodeBody(t, postWebhook(t, srv, sourcePush(0, []string{sharedRepoURL}, "def456", "lib/util.go")))

	for _, resp := range []map[string]interface{}{primary, secondary} {
		if resp["dispatched"] != true || resp["pipeline_url"] == nil || resp["pipeline_id"] == nil {
			t.Errorf("dispatch response = %v", resp)
		}
	}
	if len(capture.vars) != 2 {
		t.Fatalf("dispatched %d pipelines, want 2", len(capture.vars))
	}
	if v := capture.vars[0]; v["KESTRIX_SOURCE_NAME"] != "checkout" || v["KESTRIX_SOURCE_REVISION"] != "abc123" || v["KESTRIX_CHANGED_REPOS"] != "lib,shared" {
		t.Errorf("primary variables = %v", v)
	}
	if v := capture.vars[1]; v["KESTRIX_SOURCE_NAME"] != "shared" || v["KESTRIX_SOURCE_REVISION"] != "def456" || v["KESTRIX_CHANGED_REPOS"] != "shared" {
		t.Errorf("secondary variables = %v", v)
	}
}

// Recording sources after a webhook load is best effort: a store failure is
// logged and the delivery still resolves. Only a secondary delivery refreshes,
// so that is the one that can hit the failure.
func TestWebhookSourceRefreshFailureDoesNotFailDelivery(t *testing.T) {
	project := registeredProject(t, "checkout", unsortedSourcePipeline, 0)
	project.Sources = []domain.ProjectSource{
		{Name: "checkout", URL: project.RepoURL, Primary: true},
		{Name: "shared", URL: sharedRepoURL},
	}
	srv, store, _, _ := sourcesServer(t, project)
	store.ReplaceSourcesErr = errors.New("database unavailable")

	rec := postWebhook(t, srv, sourcePush(0, []string{sharedRepoURL}, "def456", "lib/util.go"))

	if resp := decodeBody(t, rec); rec.Code != http.StatusOK || resp["status"] != "ok" {
		t.Fatalf("status = %d response = %v", rec.Code, resp)
	}
	if len(store.ReplaceSourcesCalls) != 1 {
		t.Fatalf("refresh was not attempted: %+v", store.ReplaceSourcesCalls)
	}
	if len(project.Sources) != 2 {
		t.Errorf("a failed write updated the in-memory set: %+v", project.Sources)
	}
}

// Loading the definition re-derives sources, so a ci_repo added in Git
// becomes a source on the next load without any registration change.
func TestGetProjectDefinitionRefreshesSources(t *testing.T) {
	project := registeredProject(t, "checkout", multiSourcePipeline, 0)
	project.Sources = project.Sources[:1]
	srv, store, _, _ := sourcesServer(t, project)

	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, authed(t, srv, httptest.NewRequest(http.MethodGet, "/api/projects/checkout/definition", nil)))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if len(store.ReplaceSourcesCalls) != 1 {
		t.Fatalf("ReplaceSources calls = %+v", store.ReplaceSourcesCalls)
	}
	got := store.ReplaceSourcesCalls[0].Sources
	if len(got) != 2 || !got[0].Primary || got[1].Name != "shared" || got[1].URL != sharedRepoURL {
		t.Errorf("derived sources = %+v", got)
	}
	if len(project.Sources) != 2 {
		t.Errorf("stored project sources = %+v", project.Sources)
	}
}

// A client cannot register a source by hand: a sources field in the payload is
// accepted and ignored, and the stored set is the derived one.
func TestCreateProjectIgnoresClientSuppliedSources(t *testing.T) {
	srv := systemHandlerServer(t)
	store := srv.projectStore.(*mocks.MockProjectStore)
	body := `{"name":"demo","repo_url":"https://git.example.test/team/demo.git","repo_ref":"main",
		"sources":[{"name":"smuggled","url":"https://git.example.test/team/smuggled.git","primary":false}]}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(w, authed(t, srv, req))

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if len(store.ReplaceSourcesCalls) != 1 {
		t.Fatalf("ReplaceSources calls = %+v", store.ReplaceSourcesCalls)
	}
	got := store.ReplaceSourcesCalls[0].Sources
	if len(got) != 1 || got[0].Name != "demo" || !got[0].Primary {
		t.Errorf("stored sources = %+v, want the derived primary only", got)
	}
	if strings.Contains(w.Body.String(), "smuggled") {
		t.Errorf("response echoes the client-supplied source: %s", w.Body.String())
	}
}

// GET /api/projects/{name} exposes sources read-only: primary first, and no
// entry carries anything but name, url, and primary.
func TestGetProjectListsSources(t *testing.T) {
	project := registeredProject(t, "checkout", multiSourcePipeline, 0)
	project.RepoCredentialRef = "cred_secret"
	srv, store, _, _ := sourcesServer(t, project)
	srv.projectService = service.NewProjectService(store)

	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, authed(t, srv, httptest.NewRequest(http.MethodGet, "/api/projects/checkout", nil)))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Sources []map[string]interface{} `json:"sources"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Sources) != 2 || resp.Sources[0]["primary"] != true || resp.Sources[0]["name"] != "checkout" || resp.Sources[1]["name"] != "shared" {
		t.Fatalf("sources = %v", resp.Sources)
	}
	for _, src := range resp.Sources {
		for key := range src {
			if key != "name" && key != "url" && key != "primary" {
				t.Errorf("source carries unexpected field %q: %v", key, src)
			}
		}
	}
}

// When the parsed pipeline no longer declares the pushed repository, the
// stored source name is used so the delivery is still attributed and recorded.
func TestWebhookSecondaryChangedFallsBackToStoredName(t *testing.T) {
	project := registeredProject(t, "checkout", pipelineWithTaskTF, 0)
	srv, _, log, _ := sourcesServer(t, project)

	resp := decodeBody(t, postWebhook(t, srv, sourcePush(0, []string{sharedRepoURL}, "def456", "lib/util.go")))

	if got := stringsOf(resp["changed"]); len(got) != 1 || got[0] != "shared" {
		t.Errorf("changed = %v, want [shared]", got)
	}
	if events := log.List(); len(events) != 1 || events[0].Status != domain.WebhookStatusSkipped || events[0].SourceName != "shared" {
		t.Errorf("history = %+v", events)
	}
}
