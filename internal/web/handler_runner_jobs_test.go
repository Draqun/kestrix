package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/service"
)

var errGap = errors.New("trace offset is ahead of expected (gap)")

// contextWithRunnerID attaches a runner ID to ctx the way requireRunnerAuth
// does, for tests that call a runner-job handler directly without going
// through the middleware.
func contextWithRunnerID(ctx context.Context, id int) context.Context {
	return context.WithValue(ctx, runnerIDContextKey{}, id)
}

func setupRunnerJobServer(t *testing.T, runnerStore domain.RunnerStore, remoteJobs domain.RemoteJobStore, runStore *mocks.MockRunStore) *Server {
	t.Helper()
	if runStore == nil {
		runStore = &mocks.MockRunStore{}
	}
	pipeline := &mocks.MockPipelineRunner{}
	runService := service.NewRunService(runStore, pipeline, pipeline)
	if remoteJobs != nil {
		runService.SetRemoteJobStore(remoteJobs)
	}
	return &Server{
		cfg:               Config{ListenAddr: ":0"},
		runStore:          runStore,
		runService:        runService,
		runnerStore:       runnerStore,
		remoteJobStore:    remoteJobs,
		runnerPollLimiter: newRunnerPollLimiter(),
	}
}

// ── requireRunnerAuth (tasks.md 3.1) ────────────────────────────────

// remote-run-ux D4: the rejection says what failed. A missing or non-bearer
// header keeps its message, a bearer token the store rejects is named as such,
// and a failure to check the token is a 500, not a credential failure - the
// runner stops on 401 but must survive a database blip. "Rejects" covers an
// unknown and a revoked token alike: AuthenticateRunner reports ok=false for
// both (RunnerStore's own contract), so the middleware has no revocation check
// of its own.
func TestRequireRunnerAuth_RejectionStatusAndMessage(t *testing.T) {
	for _, tc := range []struct {
		name          string
		authorization string
		store         *mocks.MockRunnerStore
		wantStatus    int
		wantMessage   string
	}{
		{"no header", "", &mocks.MockRunnerStore{AuthenticateRunnerOK: true}, http.StatusUnauthorized, "missing or malformed Authorization header"},
		{"not a bearer token", "Basic abc123", &mocks.MockRunnerStore{AuthenticateRunnerOK: true}, http.StatusUnauthorized, "missing or malformed Authorization header"},
		{"token the store rejects", "Bearer revoked-token", &mocks.MockRunnerStore{AuthenticateRunnerOK: false}, http.StatusUnauthorized, "invalid or revoked runner token"},
		{"store failure", "Bearer any-token", &mocks.MockRunnerStore{AuthenticateRunnerErr: errors.New("db down")}, http.StatusInternalServerError, "could not check runner token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := setupRunnerJobServer(t, tc.store, &mocks.MockRemoteJobStore{}, nil)
			called := false
			h := srv.requireRunnerAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
			r := httptest.NewRequest("GET", "/api/runner-jobs/next", nil)
			if tc.authorization != "" {
				r.Header.Set("Authorization", tc.authorization)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tc.wantStatus, w.Body.String())
			}
			var body map[string]string
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] != tc.wantMessage {
				t.Fatalf("error = %q, want %q", body["error"], tc.wantMessage)
			}
			if called {
				t.Fatal("handler must not run when the token was not accepted")
			}
		})
	}
}

func TestRequireRunnerAuth_ValidTokenAccepted(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{AuthenticateRunnerID: 7, AuthenticateRunnerOK: true}, &mocks.MockRemoteJobStore{}, nil)
	var gotRunnerID int
	var ok bool
	h := srv.requireRunnerAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRunnerID, ok = runnerIDFromContext(r.Context())
	}))
	r := httptest.NewRequest("GET", "/api/runner-jobs/next", nil)
	r.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 0 && w.Code != http.StatusOK {
		t.Fatalf("status = %d, want the wrapped handler's own (default 200)", w.Code)
	}
	if !ok || gotRunnerID != 7 {
		t.Fatalf("runnerID in context = (%d, %v), want (7, true)", gotRunnerID, ok)
	}
}

func TestRequireRunnerAuth_TokenDoesNotAuthenticateAPI(t *testing.T) {
	// access-control spec's "Runner token does not authenticate /api": a
	// runner token presented to RequireAuth (the /api chain) must be
	// rejected 401 and never resolve to an account or role. RequireAuth
	// verifies a session JWT; a runner's bearer token is never one, so it
	// fails signature verification the same as any other garbage value.
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{AuthenticateRunnerID: 7, AuthenticateRunnerOK: true}, &mocks.MockRemoteJobStore{}, nil)
	h := srv.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run: a runner token is not a valid session token")
	}))
	r := httptest.NewRequest("GET", "/api/dashboard", nil)
	r.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

// ── runnerPollLimiter (tasks.md 3.1) ────────────────────────────────

func TestRunnerPollLimiter_RejectsBeyondCapAcceptsAtCap(t *testing.T) {
	l := newRunnerPollLimiter()
	const max = 2
	if !l.acquire(1, max) {
		t.Fatal("expected first acquire to succeed")
	}
	if !l.acquire(1, max) {
		t.Fatal("expected second acquire (at cap) to succeed")
	}
	if l.acquire(1, max) {
		t.Fatal("expected third acquire (beyond cap) to be rejected")
	}
	l.release(1)
	if !l.acquire(1, max) {
		t.Fatal("expected acquire to succeed again after a release")
	}
}

func TestRunnerPollLimiter_TracksPerRunnerIndependently(t *testing.T) {
	l := newRunnerPollLimiter()
	if !l.acquire(1, 1) {
		t.Fatal("expected runner 1's acquire to succeed")
	}
	if !l.acquire(2, 1) {
		t.Fatal("expected runner 2's own cap to be independent of runner 1's")
	}
}

// ── handleRunnerJobNext (tasks.md 3.2) ──────────────────────────────

func TestHandleRunnerJobNext_ClaimsPendingJob(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{ClaimNextJobResult: &domain.RemoteJob{ID: 5, TaskName: "build"}}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)
	r := httptest.NewRequest("GET", "/api/runner-jobs/next", nil)
	r = r.WithContext(contextWithRunnerID(r.Context(), 1))
	w := httptest.NewRecorder()
	srv.handleRunnerJobNext(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var got domain.RemoteJob
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID != 5 || got.TaskName != "build" {
		t.Fatalf("unexpected job: %+v", got)
	}
}

func TestHandleRunnerJobNext_NoJobReturns204AfterBoundedWait(t *testing.T) {
	old, oldTick := runnerLongPollInterval, runnerLongPollTick
	runnerLongPollInterval = 20 * time.Millisecond
	runnerLongPollTick = 5 * time.Millisecond
	defer func() { runnerLongPollInterval, runnerLongPollTick = old, oldTick }()

	remoteJobs := &mocks.MockRemoteJobStore{}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)
	r := httptest.NewRequest("GET", "/api/runner-jobs/next", nil)
	r = r.WithContext(contextWithRunnerID(r.Context(), 1))
	w := httptest.NewRecorder()
	srv.handleRunnerJobNext(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (not an error)", w.Code)
	}
}

func TestHandleRunnerJobNext_ConcurrentPollersExactlyOnceClaim(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{ClaimNextJobResult: &domain.RemoteJob{ID: 1}}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)
	// Two concurrent pollers hitting a store that (per the store's own
	// SELECT ... FOR UPDATE SKIP LOCKED semantics, exercised directly in
	// internal/store) claims exactly once: this handler-level test pins
	// that the handler itself makes exactly one ClaimNextJob call per
	// request, never retrying after a successful claim within one request.
	r1 := httptest.NewRequest("GET", "/api/runner-jobs/next", nil)
	r1 = r1.WithContext(contextWithRunnerID(r1.Context(), 1))
	w1 := httptest.NewRecorder()
	srv.handleRunnerJobNext(w1, r1)
	if w1.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w1.Code)
	}
}

func TestHandleRunnerJobNext_PollCapRejectsBeyondLimit(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)
	srv.cfg.RunnerJobMaxConcurrentPolls = 1
	old, oldTick := runnerLongPollInterval, runnerLongPollTick
	runnerLongPollInterval = 50 * time.Millisecond
	runnerLongPollTick = 5 * time.Millisecond
	defer func() { runnerLongPollInterval, runnerLongPollTick = old, oldTick }()

	done := make(chan struct{})
	go func() {
		r := httptest.NewRequest("GET", "/api/runner-jobs/next", nil)
		r = r.WithContext(contextWithRunnerID(r.Context(), 1))
		w := httptest.NewRecorder()
		srv.handleRunnerJobNext(w, r)
		close(done)
	}()
	time.Sleep(10 * time.Millisecond) // let the first poll acquire its slot

	r2 := httptest.NewRequest("GET", "/api/runner-jobs/next", nil)
	r2 = r2.WithContext(contextWithRunnerID(r2.Context(), 1))
	w2 := httptest.NewRecorder()
	srv.handleRunnerJobNext(w2, r2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (at cap)", w2.Code)
	}
	<-done
}

func TestHandleRunnerJobNext_NoRunnerIDInContext(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, &mocks.MockRemoteJobStore{}, nil)
	w := httptest.NewRecorder()
	srv.handleRunnerJobNext(w, httptest.NewRequest("GET", "/api/runner-jobs/next", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 for a request bypassing requireRunnerAuth", w.Code)
	}
}

func TestHandleRunnerJobNext_NilRemoteJobStore(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, nil, nil)
	r := httptest.NewRequest("GET", "/api/runner-jobs/next", nil)
	r = r.WithContext(contextWithRunnerID(r.Context(), 1))
	w := httptest.NewRecorder()
	srv.handleRunnerJobNext(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleRunnerJobNext_ClaimErrorPropagates(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{ClaimNextJobErr: errGap}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)
	r := httptest.NewRequest("GET", "/api/runner-jobs/next", nil)
	r = r.WithContext(contextWithRunnerID(r.Context(), 1))
	w := httptest.NewRecorder()
	srv.handleRunnerJobNext(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleRunnerJobNext_ClientDisconnectStopsThePoll(t *testing.T) {
	old, oldTick := runnerLongPollInterval, runnerLongPollTick
	runnerLongPollInterval = time.Second
	runnerLongPollTick = 5 * time.Millisecond
	defer func() { runnerLongPollInterval, runnerLongPollTick = old, oldTick }()

	remoteJobs := &mocks.MockRemoteJobStore{}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("GET", "/api/runner-jobs/next", nil).WithContext(ctx)
	r = r.WithContext(contextWithRunnerID(r.Context(), 1))
	go func() {
		time.Sleep(15 * time.Millisecond)
		cancel()
	}()
	w := httptest.NewRecorder()
	srv.handleRunnerJobNext(w, r)
	if w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" {
		t.Fatalf("expected no response body/headers written once the client disconnected, got body=%q header=%q", w.Body.String(), w.Header().Get("Content-Type"))
	}
}

// ── handleRunnerJobTrace (tasks.md 3.3) ─────────────────────────────

func traceReq(body string) *http.Request {
	r := httptest.NewRequest("PATCH", "/api/runner-jobs/1/trace", strings.NewReader(body))
	r = runCtx(r, "id", "1")
	return r.WithContext(contextWithRunnerID(r.Context(), 1))
}

func TestHandleRunnerJobTrace_RedactsKnownSecretBeforeStorage(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{AppendJobTraceOK: true}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)
	body := `{"epoch":1,"offset":0,"chunk":"token is s3cr3t-value-123"}`
	w := httptest.NewRecorder()
	srv.handleRunnerJobTrace(w, traceReq(body))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body=%s)", w.Code, w.Body.String())
	}
}

func TestHandleRunnerJobTrace_RedactsRunSecretBeforeStorage(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{AppendJobTraceOK: true, SecretValues: map[int][]string{1: {"S3cr3t-Remote-Value"}}}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)
	w := httptest.NewRecorder()
	srv.handleRunnerJobTrace(w, traceReq(`{"epoch":1,"offset":0,"chunk":"token=S3cr3t-Remote-Value\n"}`))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body=%s)", w.Code, w.Body.String())
	}
	if got := remoteJobs.AppendJobTraceChunks; len(got) != 1 || got[0] != "token=***\n" {
		t.Fatalf("stored chunks = %q, want the secret_ref value redacted", got)
	}
}

func TestHandleRunnerJobTrace_SecretLookupErrorIs500(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{AppendJobTraceOK: true, SecretValuesErr: errGap}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)
	w := httptest.NewRecorder()
	srv.handleRunnerJobTrace(w, traceReq(`{"epoch":1,"offset":0,"chunk":"x"}`))
	if w.Code != http.StatusInternalServerError || len(remoteJobs.AppendJobTraceChunks) != 0 {
		t.Fatalf("status = %d, chunks = %q; want 500 and nothing stored unredacted", w.Code, remoteJobs.AppendJobTraceChunks)
	}
}

func TestHandleRunnerJobTrace_MalformedOffsetRejected(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)
	w := httptest.NewRecorder()
	srv.handleRunnerJobTrace(w, traceReq(`{"epoch":1,"chunk":"x"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a missing offset", w.Code)
	}
}

func TestHandleRunnerJobTrace_NonNumericOffsetRejected(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)
	w := httptest.NewRecorder()
	srv.handleRunnerJobTrace(w, traceReq(`{"epoch":1,"offset":"not-a-number","chunk":"x"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a non-numeric offset", w.Code)
	}
}

func TestHandleRunnerJobTrace_StaleEpochRejected(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{AppendJobTraceOK: false}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)
	w := httptest.NewRecorder()
	srv.handleRunnerJobTrace(w, traceReq(`{"epoch":1,"offset":0,"chunk":"x"}`))
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for a stale epoch", w.Code)
	}
}

func TestHandleRunnerJobTrace_GapRejectedWithError(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{AppendJobTraceErr: errGap}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)
	w := httptest.NewRecorder()
	srv.handleRunnerJobTrace(w, traceReq(`{"epoch":1,"offset":99,"chunk":"x"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a gapped offset", w.Code)
	}
}

func TestHandleRunnerJobTrace_NilRemoteJobStore(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, nil, nil)
	w := httptest.NewRecorder()
	srv.handleRunnerJobTrace(w, traceReq(`{"epoch":1,"offset":0,"chunk":"x"}`))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleRunnerJobTrace_InvalidJobID(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, &mocks.MockRemoteJobStore{}, nil)
	r := httptest.NewRequest("PATCH", "/api/runner-jobs/not-a-number/trace", strings.NewReader(`{}`))
	r = runCtx(r, "id", "not-a-number")
	w := httptest.NewRecorder()
	srv.handleRunnerJobTrace(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// ── handleRunnerJobComplete (tasks.md 3.4, thin adapter) ────────────

func TestHandleRunnerJobComplete_OKMapsToNoContent(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{Jobs: map[int]*domain.RemoteJob{1: {ID: 1, RunID: 1, TaskName: "t"}}, CompleteJobOK: true}
	runStore := &mocks.MockRunStore{GetRunResult: &domain.Run{ID: 1}}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, runStore)
	r := httptest.NewRequest("PUT", "/api/runner-jobs/1/complete", strings.NewReader(`{"epoch":1,"status":"passed","output":"done"}`))
	r = runCtx(r, "id", "1")
	r = r.WithContext(contextWithRunnerID(r.Context(), 7))
	w := httptest.NewRecorder()
	srv.handleRunnerJobComplete(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body=%s)", w.Code, w.Body.String())
	}
}

func completeReq(body string) *http.Request {
	r := httptest.NewRequest("PUT", "/api/runner-jobs/1/complete", strings.NewReader(body))
	r = runCtx(r, "id", "1")
	return r.WithContext(contextWithRunnerID(r.Context(), 7))
}

func TestHandleRunnerJobComplete_RedactsAndBoundsError(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs:          map[int]*domain.RemoteJob{1: {ID: 1, RunID: 1, TaskName: "t"}},
		CompleteJobOK: true,
		SecretValues:  map[int][]string{1: {"S3cr3t-Remote-Value"}},
	}
	runStore := &mocks.MockRunStore{GetRunResult: &domain.Run{ID: 1}}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, runStore)
	longErr := "S3cr3t-Remote-Value " + strings.Repeat("e", 2*maxRunnerTaskErrorBytes)
	body, _ := json.Marshal(map[string]any{"epoch": 1, "status": "failed", "output": "out S3cr3t-Remote-Value", "error": longErr})
	w := httptest.NewRecorder()
	srv.handleRunnerJobComplete(w, completeReq(string(body)))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body=%s)", w.Code, w.Body.String())
	}
	call := remoteJobs.CompleteJobCalls[0]
	if call.Output != "out ***" {
		t.Fatalf("output = %q, want the secret redacted", call.Output)
	}
	if !strings.HasPrefix(call.TaskError, "*** eee") || len(call.TaskError) != maxRunnerTaskErrorBytes {
		t.Fatalf("task error = %.20q (len %d), want redacted and cut at %d bytes", call.TaskError, len(call.TaskError), maxRunnerTaskErrorBytes)
	}
}

func TestHandleRunnerJobComplete_ErrorCutKeepsValidUTF8(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{Jobs: map[int]*domain.RemoteJob{1: {ID: 1, RunID: 1, TaskName: "t"}}, CompleteJobOK: true}
	runStore := &mocks.MockRunStore{GetRunResult: &domain.Run{ID: 1}}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, runStore)
	// "ż" is two bytes; the odd prefix makes one straddle the 4 KiB cut.
	body, _ := json.Marshal(map[string]any{"epoch": 1, "status": "failed", "error": "x" + strings.Repeat("ż", maxRunnerTaskErrorBytes)})
	w := httptest.NewRecorder()
	srv.handleRunnerJobComplete(w, completeReq(string(body)))
	got := remoteJobs.CompleteJobCalls[0].TaskError
	if !utf8.ValidString(got) || len(got) != maxRunnerTaskErrorBytes-1 {
		t.Fatalf("task error len %d valid=%v, want %d valid UTF-8 bytes", len(got), utf8.ValidString(got), maxRunnerTaskErrorBytes-1)
	}
}

func TestHandleRunnerJobComplete_SecretLookupErrorIs500(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{SecretValuesErr: errGap}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)
	w := httptest.NewRecorder()
	srv.handleRunnerJobComplete(w, completeReq(`{"epoch":1,"status":"passed"}`))
	if w.Code != http.StatusInternalServerError || len(remoteJobs.CompleteJobCalls) != 0 {
		t.Fatalf("status = %d, calls = %d; want 500 and no completion recorded", w.Code, len(remoteJobs.CompleteJobCalls))
	}
}

func TestHandleRunnerJobComplete_NilRemoteJobStore(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, nil, nil)
	w := httptest.NewRecorder()
	srv.handleRunnerJobComplete(w, completeReq(`{"epoch":1,"status":"passed"}`))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleRunnerJobComplete_NotOKMapsToConflict(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{CompleteJobOK: false}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)
	r := httptest.NewRequest("PUT", "/api/runner-jobs/1/complete", strings.NewReader(`{"epoch":1,"status":"passed"}`))
	r = runCtx(r, "id", "1")
	r = r.WithContext(contextWithRunnerID(r.Context(), 7))
	w := httptest.NewRecorder()
	srv.handleRunnerJobComplete(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
}

func TestHandleRunnerJobComplete_MalformedJSON(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, &mocks.MockRemoteJobStore{}, nil)
	r := httptest.NewRequest("PUT", "/api/runner-jobs/1/complete", strings.NewReader(`{not json`))
	r = runCtx(r, "id", "1")
	r = r.WithContext(contextWithRunnerID(r.Context(), 7))
	w := httptest.NewRecorder()
	srv.handleRunnerJobComplete(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleRunnerJobComplete_NilRunService(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, &mocks.MockRemoteJobStore{}, nil)
	srv.runService = nil
	r := httptest.NewRequest("PUT", "/api/runner-jobs/1/complete", strings.NewReader(`{"epoch":1,"status":"passed"}`))
	r = runCtx(r, "id", "1")
	w := httptest.NewRecorder()
	srv.handleRunnerJobComplete(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleRunnerJobComplete_ServiceErrorPropagates(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{GetRemoteJobErr: errGap}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)
	r := httptest.NewRequest("PUT", "/api/runner-jobs/1/complete", strings.NewReader(`{"epoch":1,"status":"passed"}`))
	r = runCtx(r, "id", "1")
	r = r.WithContext(contextWithRunnerID(r.Context(), 7))
	w := httptest.NewRecorder()
	srv.handleRunnerJobComplete(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body=%s)", w.Code, w.Body.String())
	}
}

func TestHandleRunnerJobComplete_InvalidJobID(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, &mocks.MockRemoteJobStore{}, nil)
	r := httptest.NewRequest("PUT", "/api/runner-jobs/nope/complete", strings.NewReader(`{}`))
	r = runCtx(r, "id", "nope")
	w := httptest.NewRecorder()
	srv.handleRunnerJobComplete(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// ── handleCreateRunner / handleRevokeRunner (tasks.md 3.5) ──────────

func TestHandleCreateRunner_AdminSuccess(t *testing.T) {
	runnerStore := &mocks.MockRunnerStore{CreateRunnerID: 3, CreateRunnerToken: "tok-abc"}
	srv := setupRunnerJobServer(t, runnerStore, &mocks.MockRemoteJobStore{}, nil)
	body, _ := json.Marshal(map[string]string{"name": "ci-box-1"})
	r := httptest.NewRequest("POST", "/api/runners", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleCreateRunner(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["token"] != "tok-abc" {
		t.Fatalf("token = %v, want tok-abc", resp["token"])
	}
}

func TestHandleCreateRunner_MalformedJSON(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, &mocks.MockRemoteJobStore{}, nil)
	r := httptest.NewRequest("POST", "/api/runners", strings.NewReader(`{not json`))
	w := httptest.NewRecorder()
	srv.handleCreateRunner(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleCreateRunner_NilRunnerStore(t *testing.T) {
	srv := setupRunnerJobServer(t, nil, &mocks.MockRemoteJobStore{}, nil)
	body, _ := json.Marshal(map[string]string{"name": "x"})
	r := httptest.NewRequest("POST", "/api/runners", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleCreateRunner(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleCreateRunner_StoreErrorPropagates(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{CreateRunnerErr: errGap}, &mocks.MockRemoteJobStore{}, nil)
	body, _ := json.Marshal(map[string]string{"name": "x"})
	r := httptest.NewRequest("POST", "/api/runners", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleCreateRunner(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleCreateRunner_MissingName(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, &mocks.MockRemoteJobStore{}, nil)
	r := httptest.NewRequest("POST", "/api/runners", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	srv.handleCreateRunner(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleRevokeRunner_AdminSuccess(t *testing.T) {
	runnerStore := &mocks.MockRunnerStore{}
	srv := setupRunnerJobServer(t, runnerStore, &mocks.MockRemoteJobStore{}, nil)
	r := httptest.NewRequest("DELETE", "/api/runners/3", nil)
	r = runCtx(r, "id", "3")
	w := httptest.NewRecorder()
	srv.handleRevokeRunner(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if len(runnerStore.RevokeRunnerCalls) != 1 || runnerStore.RevokeRunnerCalls[0] != 3 {
		t.Fatalf("RevokeRunnerCalls = %v, want [3]", runnerStore.RevokeRunnerCalls)
	}
}

func TestHandleRevokeRunner_NilRunnerStore(t *testing.T) {
	srv := setupRunnerJobServer(t, nil, &mocks.MockRemoteJobStore{}, nil)
	r := httptest.NewRequest("DELETE", "/api/runners/1", nil)
	r = runCtx(r, "id", "1")
	w := httptest.NewRecorder()
	srv.handleRevokeRunner(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleRevokeRunner_StoreErrorPropagates(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{RevokeRunnerErr: errGap}, &mocks.MockRemoteJobStore{}, nil)
	r := httptest.NewRequest("DELETE", "/api/runners/1", nil)
	r = runCtx(r, "id", "1")
	w := httptest.NewRecorder()
	srv.handleRevokeRunner(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleRevokeRunner_InvalidID(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, &mocks.MockRemoteJobStore{}, nil)
	r := httptest.NewRequest("DELETE", "/api/runners/nope", nil)
	r = runCtx(r, "id", "nope")
	w := httptest.NewRecorder()
	srv.handleRevokeRunner(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// TestRunnerRoutesRequireAdminRole and the non-admin 403 cases are covered
// through the router-level RequireRole tests in server_admin_test.go-style
// suites elsewhere; these two confirm the routes are wired under the admin
// group at all, using the real router.
func TestRunnerRoutesWiredUnderAdminGroup(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, &mocks.MockRemoteJobStore{}, nil)
	srv.router = srv.buildRouter()
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/runners"},
		{"DELETE", "/api/runners/1"},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, r)
		if w.Code == http.StatusNotFound {
			t.Fatalf("%s %s: route not found, want it registered under /api", tc.method, tc.path)
		}
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: status = %d, want 401 (no auth presented, but the route must exist and require auth)", tc.method, tc.path, w.Code)
		}
	}
}

// TestRunnerJobMaxBodyBytes_RejectsOverAcceptsAtLimit pins tasks.md 3.1: the
// bound rejects only what exceeds it, paired at exactly the limit.
func TestRunnerJobMaxBodyBytes_RejectsOverAcceptsAtLimit(t *testing.T) {
	srv := NewServer(Config{RunnerJobMaxBodyBytes: 64})
	srv.runnerStore = &mocks.MockRunnerStore{AuthenticateRunnerOK: true}
	srv.remoteJobStore = &mocks.MockRemoteJobStore{AppendJobTraceOK: true}
	srv.router = srv.buildRouter()

	// A chunk sized so the full JSON body is exactly 64 bytes.
	const prefix, suffix = `{"epoch":1,"offset":0,"chunk":"`, `"}`
	atLimitChunk := strings.Repeat("a", 64-len(prefix)-len(suffix))
	body := prefix + atLimitChunk + suffix
	if len(body) != 64 {
		t.Fatalf("test setup: body is %d bytes, want exactly 64", len(body))
	}
	r := httptest.NewRequest("PATCH", "/api/runner-jobs/1/trace", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, r)
	if w.Code == http.StatusRequestEntityTooLarge {
		t.Fatalf("at-limit body (64 bytes) rejected, want it accepted (body=%s)", w.Body.String())
	}

	over := prefix + atLimitChunk + "x" + suffix
	r2 := httptest.NewRequest("PATCH", "/api/runner-jobs/1/trace", strings.NewReader(over))
	r2.Header.Set("Authorization", "Bearer valid-token")
	w2 := httptest.NewRecorder()
	srv.router.ServeHTTP(w2, r2)
	if w2.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over-limit body: status = %d, want 413", w2.Code)
	}
}

// TestRunnerRoutes_NonAdminRejected pins access-control's "Runner
// management requires admin role" for both POST /api/runners and
// DELETE /api/runners/{id}.
func TestRunnerRoutes_NonAdminRejected(t *testing.T) {
	srv := NewServer(Config{})
	srv.runnerStore = &mocks.MockRunnerStore{}
	srv.router = srv.buildRouter()

	if _, err := srv.userStore.CreateUser("viewer", domain.RoleViewer, "x"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token, _, err := srv.tokenIssuer.Issue(domain.User{Name: "viewer", Role: domain.RoleViewer, TokenVersion: 1})
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/api/runners", `{"name":"x"}`},
		{"DELETE", "/api/runners/1", ""},
	} {
		var bodyReader *strings.Reader
		if tc.body != "" {
			bodyReader = strings.NewReader(tc.body)
		} else {
			bodyReader = strings.NewReader("")
		}
		r := httptest.NewRequest(tc.method, tc.path, bodyReader)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s %s: status = %d, want 403 for a non-admin (body=%s)", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestRunnerJobRoutesWiredOutsideAPIAuth(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, &mocks.MockRemoteJobStore{}, nil)
	srv.router = srv.buildRouter()
	r := httptest.NewRequest("GET", "/api/runner-jobs/next", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (requireRunnerAuth, not RequireAuth)", w.Code)
	}
}

// ── remote-runner-management: tags and GET /api/runners ────────────

func TestHandleCreateRunner_NormalisesAndEchoesTags(t *testing.T) {
	runnerStore := &mocks.MockRunnerStore{CreateRunnerID: 4, CreateRunnerToken: "tok"}
	srv := setupRunnerJobServer(t, runnerStore, &mocks.MockRemoteJobStore{}, nil)
	body, _ := json.Marshal(map[string]any{"name": "gpu-box", "tags": []string{"GPU", " gpu", "linux"}})
	w := httptest.NewRecorder()
	srv.handleCreateRunner(w, httptest.NewRequest("POST", "/api/runners", bytes.NewReader(body)))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var resp runnerCreateResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	want := []string{"gpu", "linux"}
	if !reflect.DeepEqual(resp.Tags, want) || !reflect.DeepEqual(runnerStore.CreateRunnerTags, want) {
		t.Fatalf("response tags %q, stored tags %q, want %q", resp.Tags, runnerStore.CreateRunnerTags, want)
	}
}

func TestHandleCreateRunner_UntaggedEchoesEmptyList(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{CreateRunnerID: 5}, &mocks.MockRemoteJobStore{}, nil)
	w := httptest.NewRecorder()
	srv.handleCreateRunner(w, httptest.NewRequest("POST", "/api/runners", strings.NewReader(`{"name":"plain"}`)))
	if !strings.Contains(w.Body.String(), `"tags":[]`) {
		t.Fatalf("body = %s, want tags as an empty list", w.Body.String())
	}
}

func TestHandleCreateRunner_InvalidTagRejected(t *testing.T) {
	runnerStore := &mocks.MockRunnerStore{}
	srv := setupRunnerJobServer(t, runnerStore, &mocks.MockRemoteJobStore{}, nil)
	w := httptest.NewRecorder()
	srv.handleCreateRunner(w, httptest.NewRequest("POST", "/api/runners", strings.NewReader(`{"name":"x","tags":["has space"]}`)))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "has space") {
		t.Fatalf("status = %d body = %s, want 400 naming the tag", w.Code, w.Body.String())
	}
	if runnerStore.CreateRunnerTags != nil {
		t.Fatal("a runner was created despite the invalid tag")
	}
}

func TestHandleListRunners_StatusesAndNoSecrets(t *testing.T) {
	now := time.Now()
	recent, stale := now.Add(-5*time.Second), now.Add(-10*time.Minute)
	runnerStore := &mocks.MockRunnerStore{ListRunnersResult: []domain.Runner{
		{ID: 1, Name: "busy", Tags: []string{"gpu"}, LastSeenAt: &stale, Busy: true},
		{ID: 2, Name: "online", LastSeenAt: &recent},
		{ID: 3, Name: "offline", LastSeenAt: &stale},
		{ID: 4, Name: "new"},
	}}
	srv := setupRunnerJobServer(t, runnerStore, &mocks.MockRemoteJobStore{}, nil)
	w := httptest.NewRecorder()
	srv.handleListRunners(w, httptest.NewRequest("GET", "/api/runners", nil))
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(bytes.NewReader(w.Body.Bytes())).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	want := []string{"busy", "online", "offline", "never seen"}
	for i, item := range resp.Items {
		if item["status"] != want[i] {
			t.Fatalf("runner %v status = %v, want %s", item["name"], item["status"], want[i])
		}
		for _, secret := range []string{"token", "token_hash", "busy"} {
			if _, present := item[secret]; present {
				t.Fatalf("runner %v exposes %q", item["name"], secret)
			}
		}
	}
	if tags := resp.Items[1]["tags"]; tags == nil {
		t.Fatal("an untagged runner's tags should be an empty list, not null")
	}
}

func TestHandleListRunners_Errors(t *testing.T) {
	srv := setupRunnerJobServer(t, nil, &mocks.MockRemoteJobStore{}, nil)
	w := httptest.NewRecorder()
	srv.handleListRunners(w, httptest.NewRequest("GET", "/api/runners", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("nil store: status = %d, want 500", w.Code)
	}
	srv = setupRunnerJobServer(t, &mocks.MockRunnerStore{ListRunnersErr: errGap}, &mocks.MockRemoteJobStore{}, nil)
	w = httptest.NewRecorder()
	srv.handleListRunners(w, httptest.NewRequest("GET", "/api/runners", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("store error: status = %d, want 500", w.Code)
	}
}

func TestListRunnersRoute_AdminOnly(t *testing.T) {
	srv := NewServer(Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/runners", nil)
	req.Header.Set("Authorization", authToken(t, srv, domain.RoleOperator))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("operator GET /api/runners = %d, want 403", w.Code)
	}
}

func TestServer_RunnerOfflineAfterDefault(t *testing.T) {
	srv := &Server{cfg: Config{ListenAddr: ":0"}}
	if got := srv.runnerOfflineAfter(); got != defaultRunnerOfflineAfter {
		t.Fatalf("default = %v, want %v", got, defaultRunnerOfflineAfter)
	}
	srv.cfg.Runner.OfflineAfter = 2 * time.Minute
	if got := srv.runnerOfflineAfter(); got != 2*time.Minute {
		t.Fatalf("configured = %v, want 2m", got)
	}
}
