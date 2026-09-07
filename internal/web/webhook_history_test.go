package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/web/pagination"
)

// postWebhook sends a push payload through the router and returns the recorder.
func postWebhook(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/webhook", io.NopCloser(bytes.NewBufferString(body)))
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)
	return rec
}

func historyServer(t *testing.T) (*Server, *mocks.MockWebhookLogSink, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pipeline.tf"), []byte(pipelineWithTaskTF), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Config{ListenAddr: ":0", PipelineDir: dir})
	return srv, testWebhookLog(t, srv), dir
}

// An event that matches no registered project is still recorded, and the
// caller still gets its ignored response.
func TestWebhookRecordsIgnoredEvent(t *testing.T) {
	srv, log, _ := historyServer(t)

	rec := postWebhook(t, srv, `{"object_kind":"push","ref":"refs/heads/main","checkout_sha":"abc123",
		"project":{"id":4242,"path_with_namespace":"org/unknown","default_branch":"main"},
		"commits":[{"id":"abc123","added":[],"modified":["svc/main.go"],"removed":[]}]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "ignored" {
		t.Errorf("response status = %v, want ignored", resp["status"])
	}

	events := log.List()
	if len(events) != 1 {
		t.Fatalf("recorded %d events, want 1", len(events))
	}
	if events[0].Status != domain.WebhookStatusIgnored {
		t.Errorf("recorded status = %q, want %q", events[0].Status, domain.WebhookStatusIgnored)
	}
	if events[0].ProjectName != "" {
		t.Errorf("recorded project = %q, want empty", events[0].ProjectName)
	}
	if events[0].Repo != "org/unknown" {
		t.Errorf("recorded repo = %q, want org/unknown", events[0].Repo)
	}
}

// A resolved event that triggers nothing is recorded as skipped.
func TestWebhookRecordsSkippedEvent(t *testing.T) {
	srv, log, _ := historyServer(t)

	rec := postWebhook(t, srv, `{"object_kind":"push","ref":"refs/heads/main","checkout_sha":"abc123",
		"commits":[{"id":"abc123","added":[],"modified":["docs/README.md"],"removed":[]}]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	events := log.List()
	if len(events) != 1 {
		t.Fatalf("recorded %d events, want 1", len(events))
	}
	if events[0].Status != domain.WebhookStatusSkipped {
		t.Errorf("recorded status = %q, want %q", events[0].Status, domain.WebhookStatusSkipped)
	}
	if len(events[0].TriggeredTasks) != 0 {
		t.Errorf("triggered tasks = %v, want none", events[0].TriggeredTasks)
	}
}

// A processing failure is recorded with its message.
func TestWebhookRecordsErrorEvent(t *testing.T) {
	srv, log, dir := historyServer(t)
	if err := os.WriteFile(filepath.Join(dir, "pipeline.tf"), []byte("project \"broken\" {"), 0o600); err != nil {
		t.Fatal(err)
	}

	postWebhook(t, srv, `{"object_kind":"push","ref":"refs/heads/main","checkout_sha":"abc123",
		"commits":[{"id":"abc123","added":[],"modified":["svc/main.go"],"removed":[]}]}`)

	events := log.List()
	if len(events) != 1 {
		t.Fatalf("recorded %d events, want 1", len(events))
	}
	if events[0].Status != domain.WebhookStatusError {
		t.Errorf("recorded status = %q, want %q", events[0].Status, domain.WebhookStatusError)
	}
	if events[0].Error == "" {
		t.Error("recorded event carries no error message")
	}
}

// A history store outage must not change the response the receiver returns:
// GitLab retries on non-2xx, so a reporting failure would become a delivery
// failure.
func TestWebhookRecordingFailureDoesNotFailRequest(t *testing.T) {
	srv, log, _ := historyServer(t)
	log.RecordErr = errors.New("database unavailable")

	rec := postWebhook(t, srv, `{"object_kind":"push","ref":"refs/heads/main","checkout_sha":"abc123",
		"commits":[{"id":"abc123","added":[],"modified":["svc/main.go"],"removed":[]}]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(log.List()) != 0 {
		t.Errorf("failing store recorded %d events, want 0", len(log.List()))
	}
}

func TestHandleGetLogs_Filters(t *testing.T) {
	srv := systemHandlerServer(t)
	log := testWebhookLog(t, srv)
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	for i, e := range []domain.WebhookEvent{
		{ProjectName: "web", Repo: "org/web", Status: domain.WebhookStatusTriggered},
		{ProjectName: "web", Repo: "org/web", Status: domain.WebhookStatusError, Error: "parse failed"},
		{ProjectName: "api", Repo: "org/api", Status: domain.WebhookStatusError, Error: "parse failed"},
	} {
		e.ReceivedAt = base.Add(time.Duration(i) * time.Minute)
		if err := log.Record(e); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	cases := []struct {
		name  string
		query string
		want  int
	}{
		{"project", "?project=web", 2},
		{"status", "?status=error", 2},
		{"conjunction", "?project=web&status=error", 1},
		{"since", "?since=" + base.Add(time.Minute).Format(time.RFC3339), 2},
		{"until", "?until=" + base.Add(time.Minute).Format(time.RFC3339), 2},
		{"range", "?since=" + base.Add(time.Minute).Format(time.RFC3339) +
			"&until=" + base.Add(time.Minute).Format(time.RFC3339), 1},
		{"no match", "?project=missing", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			srv.handleGetLogs(w, httptest.NewRequest("GET", "/api/logs"+tc.query, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
			}
			var resp struct {
				Items      []domain.WebhookEvent `json:"items"`
				Pagination pagination.Meta       `json:"pagination"`
			}
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(resp.Items) != tc.want || resp.Pagination.Total != tc.want {
				t.Errorf("items = %d, total = %d, want %d", len(resp.Items), resp.Pagination.Total, tc.want)
			}
		})
	}
}

func TestHandleGetLogs_InvalidParameters(t *testing.T) {
	srv := systemHandlerServer(t)
	cases := []struct{ name, query string }{
		{"malformed since", "?since=yesterday"},
		{"malformed until", "?until=2026-09-01"},
		{"unknown status", "?status=exploded"},
		{"non-numeric limit", "?limit=many"},
		{"disallowed limit", "?limit=7"},
		{"negative limit", "?limit=-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			srv.handleGetLogs(w, httptest.NewRequest("GET", "/api/logs"+tc.query, nil))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", w.Code)
			}
			if bytes.Contains(w.Body.Bytes(), []byte(`"items"`)) {
				t.Errorf("rejected request still returned events: %s", w.Body.String())
			}
		})
	}
}

// handleHealth must report the stored history size and the registered project
// count. Before this change both fields echoed the same in-memory slice length.
func TestHandleHealthReportsDistinctCounts(t *testing.T) {
	srv := systemHandlerServer(t)
	log := testWebhookLog(t, srv)
	for i := 0; i < 3; i++ {
		if err := log.Record(domain.WebhookEvent{Repo: "org/demo", Status: domain.WebhookStatusTriggered}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	if err := srv.projectStore.Upsert(&domain.ProjectConfig{
		Name: "demo", RepoURL: "https://git.example/demo.git", RepoRef: "main",
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	w := httptest.NewRecorder()
	srv.handleHealth(w, httptest.NewRequest("GET", "/api/health", nil))

	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["webhooks"] != float64(3) {
		t.Errorf("webhooks = %v, want 3", body["webhooks"])
	}
	if body["pipelines"] != float64(1) {
		t.Errorf("pipelines = %v, want 1", body["pipelines"])
	}
}

func TestHandleGetLogs_StoreFailures(t *testing.T) {
	cases := []struct {
		name  string
		apply func(*mocks.MockWebhookLogSink)
	}{
		{"count fails", func(l *mocks.MockWebhookLogSink) { l.CountErr = errors.New("database unavailable") }},
		{"list fails", func(l *mocks.MockWebhookLogSink) { l.ListErr = errors.New("database unavailable") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := systemHandlerServer(t)
			tc.apply(testWebhookLog(t, srv))

			w := httptest.NewRecorder()
			srv.handleGetLogs(w, httptest.NewRequest("GET", "/api/logs", nil))

			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", w.Code)
			}
			if !bytes.Contains(w.Body.Bytes(), []byte("read webhook history")) {
				t.Errorf("body = %s, want a history read error", w.Body.String())
			}
		})
	}
}

// webhookAwareStore is a project store that also serves as webhook history and
// accepts a retention limit, mirroring what PGStore provides at runtime.
type webhookAwareStore struct {
	*mocks.MockProjectStore
	history *mocks.MockWebhookLogSink
	limit   int
}

func (s *webhookAwareStore) SetWebhookHistoryLimit(limit int) { s.limit = limit }

func (s *webhookAwareStore) Record(event domain.WebhookEvent) error {
	return s.history.Record(event)
}

func (s *webhookAwareStore) ListWebhookEvents(filter domain.WebhookEventFilter, limit, offset int) ([]domain.WebhookEvent, error) {
	return s.history.ListWebhookEvents(filter, limit, offset)
}

func (s *webhookAwareStore) CountWebhookEvents(filter domain.WebhookEventFilter) (int, error) {
	return s.history.CountWebhookEvents(filter)
}

// NewServerE must recognise a store that also implements webhook history and
// hand it the configured retention limit.
func TestNewServerEWiresWebhookHistory(t *testing.T) {
	projectStore := &webhookAwareStore{
		MockProjectStore: &mocks.MockProjectStore{},
		history:          &mocks.MockWebhookLogSink{},
	}
	runStore := &mocks.MockRunStore{}
	old := newPGStores
	newPGStores = func(string) (domain.ProjectStore, domain.RunStore, error) {
		return projectStore, runStore, nil
	}
	t.Cleanup(func() { newPGStores = old })

	server, err := NewServerE(Config{
		DatabaseURL:         "postgres://valid",
		MasterKeyPath:       validMasterKeyPath(t),
		JWTSigningKeyPath:   validSigningKeyPath(t),
		WebhookHistoryLimit: 250,
	})
	if err != nil {
		t.Fatalf("NewServerE: %v", err)
	}
	if server.webhookLogStore == nil {
		t.Fatal("webhook history store was not wired")
	}
	if projectStore.limit != 250 {
		t.Errorf("retention limit = %d, want 250", projectStore.limit)
	}

	server.webhookService.Record(domain.WebhookEvent{Repo: "org/demo", Status: domain.WebhookStatusTriggered})
	if len(projectStore.history.List()) != 1 {
		t.Error("webhook sink was not wired to the store")
	}
}

// A caller that supplies no webhook service still gets a usable server; the
// service is constructed without a sink, so recording is a no-op.
func TestNewServerWithDepsDefaultsWebhookService(t *testing.T) {
	projectStore := &mocks.MockProjectStore{}
	runStore := &mocks.MockRunStore{}
	srv := NewServerWithDeps(Config{ListenAddr: ":0"}, projectStore, runStore, nil, nil, nil, nil, nil)

	if srv.webhookService == nil {
		t.Fatal("webhook service was not defaulted")
	}
	srv.webhookService.Record(domain.WebhookEvent{Repo: "org/demo", Status: domain.WebhookStatusSkipped})
}

// An event matched to a registered project carries that project's name, so the
// project filter and the Project column have something to work with.
func TestWebhookRecordsMatchedProjectName(t *testing.T) {
	srv, log, dir := historyServer(t)
	srv.projectStore = &mocks.MockProjectStore{FindByGitLabIDResult: &domain.ProjectConfig{
		Name: "billing", RepoURL: "file://" + dir, RepoRef: "main",
		PipelineDir: ".", GitLabProjectID: 4242,
	}}
	srv.sourceSessions = &testSourceSessions{}

	rec := postWebhook(t, srv, `{"object_kind":"push","ref":"refs/heads/main","checkout_sha":"abc123",
		"project":{"id":4242,"path_with_namespace":"org/billing","default_branch":"main"},
		"commits":[{"id":"abc123","added":[],"modified":["src/main.go"],"removed":[]}]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	events := log.List()
	if len(events) != 1 {
		t.Fatalf("recorded %d events, want 1", len(events))
	}
	if events[0].ProjectName != "billing" {
		t.Errorf("recorded project = %q, want billing", events[0].ProjectName)
	}
	if events[0].Status != domain.WebhookStatusTriggered {
		t.Errorf("recorded status = %q, want %q", events[0].Status, domain.WebhookStatusTriggered)
	}
}

// A dispatch failure is a processing failure: the event carries status error so
// that status=error finds it, rather than a success status with an error field.
func TestWebhookRecordsDispatchFailure(t *testing.T) {
	srv, log, _ := historyServer(t)
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer failing.Close()
	srv.cfg.Dispatch = DispatchConfig{Enabled: true, ProjectID: "12", GitLabURL: failing.URL}
	srv.cfg.Branch = "main"

	rec := postWebhook(t, srv, `{"object_kind":"push","ref":"refs/heads/main","checkout_sha":"abc123",
		"commits":[{"id":"abc123","added":[],"modified":["src/main.go"],"removed":[]}]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	events := log.List()
	if len(events) != 1 {
		t.Fatalf("recorded %d events, want 1", len(events))
	}
	if events[0].Status != domain.WebhookStatusError {
		t.Errorf("recorded status = %q, want %q", events[0].Status, domain.WebhookStatusError)
	}
	if events[0].Error == "" {
		t.Error("recorded event carries no dispatch error message")
	}
	if len(events[0].TriggeredTasks) == 0 {
		t.Error("recorded event lists no triggered tasks")
	}
}

// A store failure must not be reported as a healthy zero count: the counter is
// omitted and the status degrades.
func TestHandleHealthDegradesOnStoreFailure(t *testing.T) {
	cases := []struct {
		name     string
		apply    func(*Server)
		omitted  string
		retained string
	}{
		{
			name: "webhook count fails",
			apply: func(srv *Server) {
				testWebhookLog(t, srv).CountErr = errors.New("count down")
			},
			omitted:  "webhooks",
			retained: "pipelines",
		},
		{
			name: "project list fails",
			apply: func(srv *Server) {
				srv.projectStore = &mocks.MockProjectStore{ListErr: errors.New("list down")}
			},
			omitted:  "pipelines",
			retained: "webhooks",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := systemHandlerServer(t)
			tc.apply(srv)

			w := httptest.NewRecorder()
			srv.handleHealth(w, httptest.NewRequest("GET", "/api/health", nil))

			var body map[string]interface{}
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("code = %d, want %d", w.Code, http.StatusServiceUnavailable)
			}
			if body["status"] != "degraded" {
				t.Errorf("status = %v, want degraded", body["status"])
			}
			if _, ok := body[tc.omitted]; ok {
				t.Errorf("%s reported as %v, want omitted", tc.omitted, body[tc.omitted])
			}
			if _, ok := body[tc.retained]; !ok {
				t.Errorf("%s missing, want reported", tc.retained)
			}
		})
	}
}

// Without a store the health counters still report zero rather than vanish.
func TestHandleHealthWithoutStores(t *testing.T) {
	srv := &Server{}
	w := httptest.NewRecorder()
	srv.handleHealth(w, httptest.NewRequest("GET", "/api/health", nil))

	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", w.Code)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
	if body["webhooks"] != float64(0) || body["pipelines"] != float64(0) {
		t.Errorf("counters = %v/%v, want 0/0", body["webhooks"], body["pipelines"])
	}
}

// The response of POST /webhook is a contract of its own. Its failure paths
// report the recorded event, so the history type must not leak its field names
// into that document when it changes.
func TestWebhookErrorResponseKeepsItsShape(t *testing.T) {
	srv, _, dir := historyServer(t)
	if err := os.WriteFile(filepath.Join(dir, "pipeline.tf"), []byte("project \"broken\" {"), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := postWebhook(t, srv, `{"object_kind":"push","ref":"refs/heads/main","checkout_sha":"abc123",
		"project":{"path_with_namespace":"org/web","default_branch":"main"},
		"commits":[{"id":"abc123","added":[],"modified":["svc/main.go"],"removed":[]}]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"timestamp", "repo", "branch", "revision", "commits", "tasks", "status", "error"} {
		if _, ok := resp[key]; !ok {
			t.Errorf("response is missing %q: %v", key, resp)
		}
	}
	for _, key := range []string{"received_at", "triggered_tasks", "id", "project_name"} {
		if _, ok := resp[key]; ok {
			t.Errorf("response carries the history field %q: %v", key, resp)
		}
	}
	if resp["status"] != domain.WebhookStatusError {
		t.Errorf("status = %v, want %q", resp["status"], domain.WebhookStatusError)
	}
	if resp["repo"] != "org/web" {
		t.Errorf("repo = %v, want org/web", resp["repo"])
	}
}

// A store lookup failure takes the other path through the same response.
func TestWebhookStoreFailureResponseKeepsItsShape(t *testing.T) {
	srv, _, _ := historyServer(t)
	srv.projectStore = &mocks.MockProjectStore{FindByGitLabIDErr: errors.New("database unavailable")}

	rec := postWebhook(t, srv, `{"object_kind":"push","ref":"refs/heads/main","checkout_sha":"abc123",
		"project":{"id":4242,"path_with_namespace":"org/web","default_branch":"main"},
		"commits":[{"id":"abc123","added":[],"modified":["svc/main.go"],"removed":[]}]}`)

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["timestamp"]; !ok {
		t.Errorf("response is missing timestamp: %v", resp)
	}
	if _, ok := resp["received_at"]; ok {
		t.Errorf("response carries the history field received_at: %v", resp)
	}
	if resp["error"] != "database unavailable" {
		t.Errorf("error = %v, want database unavailable", resp["error"])
	}
}

// Without a registered project the pipeline directory still names the project,
// so history records it rather than leaving every row blank.
func TestWebhookRecordsParsedProjectName(t *testing.T) {
	srv, log, _ := historyServer(t)

	postWebhook(t, srv, `{"object_kind":"push","ref":"refs/heads/main","checkout_sha":"abc123",
		"commits":[{"id":"abc123","added":[],"modified":["src/main.go"],"removed":[]}]}`)

	events := log.List()
	if len(events) != 1 {
		t.Fatalf("recorded %d events, want 1", len(events))
	}
	if events[0].ProjectName != "demo" {
		t.Errorf("recorded project name = %q, want demo", events[0].ProjectName)
	}
}
