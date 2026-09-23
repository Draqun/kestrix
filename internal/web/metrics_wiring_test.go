package web

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"kestrix/internal/metrics"
	"kestrix/internal/service"

	"kestrix/internal/mocks"
)

// metricsTestRecorder is a minimal metrics.Recorder double shared by tests
// in this package that need to assert what a handler recorded, without
// depending on internal/metrics.Registry's own internals.
type metricsTestRecorder struct {
	mu   sync.Mutex
	runs []metricsTestRunFinished
	hits []string
}

type metricsTestRunFinished struct {
	status   string
	project  string
	duration time.Duration
}

func (r *metricsTestRecorder) RunFinished(status, project string, d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs = append(r.runs, metricsTestRunFinished{status: status, project: project, duration: d})
}

func (r *metricsTestRecorder) WebhookDelivery(outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hits = append(r.hits, outcome)
}

func (r *metricsTestRecorder) snapshot() []metricsTestRunFinished {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]metricsTestRunFinished(nil), r.runs...)
}

func (r *metricsTestRecorder) outcomes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.hits...)
}

// TestNewServerWithDeps_MetricsDisabledByDefault covers observability
// spec.md "Metrics disabled by default": with no Metrics.Enabled setting,
// the server's Recorder is a no-op, and so is the one wired into RunService.
func TestNewServerWithDeps_MetricsDisabledByDefault(t *testing.T) {
	srv := NewServer(Config{ListenAddr: ":0", Runner: RunnerConfig{Mode: "local"}})
	if _, ok := srv.metrics.(metrics.NoOp); !ok {
		t.Fatalf("metrics = %T, want metrics.NoOp", srv.metrics)
	}
	if _, ok := srv.runService.Metrics.(metrics.NoOp); !ok {
		t.Fatalf("runService.Metrics = %T, want metrics.NoOp", srv.runService.Metrics)
	}
}

// TestNewServerWithDeps_MetricsEnabledUsesRealRegistry covers the enabled
// branch: a real *metrics.Registry backs both the server and the RunService
// it wires, sharing one instance so a run recorded via RunService is visible
// on the same registry the /metrics handler reads.
func TestNewServerWithDeps_MetricsEnabledUsesRealRegistry(t *testing.T) {
	srv := NewServer(Config{ListenAddr: ":0", Runner: RunnerConfig{Mode: "local"}, Metrics: MetricsConfig{Enabled: true}})
	reg, ok := srv.metrics.(*metrics.Registry)
	if !ok {
		t.Fatalf("metrics = %T, want *metrics.Registry", srv.metrics)
	}
	if srv.runService.Metrics != reg {
		t.Fatalf("runService.Metrics = %v, want the same *metrics.Registry instance as srv.metrics", srv.runService.Metrics)
	}

	reg.RunFinished("passed", "demo", time.Second)
	snap := reg.Snapshot(0, 0, "test")
	if snap.RunTotal["passed"]["demo"] != 1 {
		t.Fatalf("recorded through srv.metrics, read back = %+v", snap.RunTotal)
	}
}

// TestWebhookDelivery_RecordsOutcomes covers tasks.md 4.3: accepted (a task
// triggers), skipped (no task triggers), and rejected (signature
// verification fails) each record their own outcome.
func TestWebhookDelivery_RecordsOutcomes(t *testing.T) {
	srv, _, _ := historyServer(t)
	rec := &metricsTestRecorder{}
	srv.metrics = rec

	// Accepted: modifying src/ (pipelineWithTaskTF's "svc" repo path) matches
	// repo.svc.changed and triggers "build" - see
	// TestWebhookRecordsMatchedProjectName, which asserts
	// WebhookStatusTriggered against the same pipeline for this same path.
	if got := postWebhook(t, srv, `{"object_kind":"push","ref":"refs/heads/main","checkout_sha":"abc123",
		"commits":[{"id":"abc123","added":[],"modified":["src/main.go"],"removed":[]}]}`); got.Code != http.StatusOK {
		t.Fatalf("accepted case: status = %d, want 200 (body=%s)", got.Code, got.Body.String())
	}

	// Skipped: modifying a path outside src/ triggers nothing - see
	// TestWebhookRecordsSkippedEvent for the same fixture and path.
	if got := postWebhook(t, srv, `{"object_kind":"push","ref":"refs/heads/main","checkout_sha":"def456",
		"commits":[{"id":"def456","added":[],"modified":["docs/README.md"],"removed":[]}]}`); got.Code != http.StatusOK {
		t.Fatalf("skipped case: status = %d, want 200 (body=%s)", got.Code, got.Body.String())
	}

	// Rejected: wrong token fails signature verification.
	if got := postWebhookWithToken(t, srv, `{"object_kind":"push","ref":"refs/heads/main","checkout_sha":"ghi789",
		"commits":[{"id":"ghi789","added":[],"modified":["src/main.go"],"removed":[]}]}`, "wrong-token"); got.Code != http.StatusUnauthorized {
		t.Fatalf("rejected case: status = %d, want 401", got.Code)
	}

	outcomes := rec.outcomes()
	if len(outcomes) != 3 {
		t.Fatalf("outcomes = %v, want 3 entries", outcomes)
	}
	if outcomes[0] != "accepted" || outcomes[1] != "skipped" || outcomes[2] != "rejected" {
		t.Fatalf("outcomes = %v, want [accepted skipped rejected]", outcomes)
	}
}

// TestNewServerWithDeps_NilRunServiceDoesNotPanic covers the runService-nil
// guard: wiring metrics must not assume a RunService is always present.
func TestNewServerWithDeps_NilRunServiceDoesNotPanic(t *testing.T) {
	projectStore := &mocks.MockProjectStore{}
	runStore := &mocks.MockRunStore{}
	srv := NewServerWithDeps(
		Config{ListenAddr: ":0", Metrics: MetricsConfig{Enabled: true}},
		projectStore,
		runStore,
		service.NewProjectService(projectStore),
		nil, // pipelineRunner
		nil, // taskRunner
		nil, // runService - the case under test
		nil, // webhookService
	)
	if srv.metrics == nil {
		t.Fatal("metrics = nil, want a Recorder even with runService nil")
	}
}
