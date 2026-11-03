package web

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/service"
)

func newSSETestServer(t *testing.T, runStore domain.RunStore) *Server {
	t.Helper()
	cfg := Config{ListenAddr: ":0"}
	if runStore == nil {
		runStore = &mocks.MockRunStore{}
	}
	s := &Server{
		cfg:      cfg,
		runStore: runStore,
		eventHub: service.NewRunEventHub(),
	}
	return s
}

func sseRequest(ctx context.Context, projectName, runID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/projects/"+projectName+"/runs/"+runID+"/events", nil)
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	chiCtx := chi.NewRouteContext()
	chiCtx.URLParams.Add("name", projectName)
	chiCtx.URLParams.Add("runID", runID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, chiCtx))
}

func TestHandlerSSE_ValidationAndNotFound(t *testing.T) {
	srv := newSSETestServer(t, nil)

	// Invalid run ID
	w := httptest.NewRecorder()
	srv.handleRunEvents(w, sseRequest(context.Background(), "my-proj", "abc"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}

	// Run not found
	mockStore := &mocks.MockRunStore{
		GetRunResult: nil,
	}
	srv = newSSETestServer(t, mockStore)
	w = httptest.NewRecorder()
	srv.handleRunEvents(w, sseRequest(context.Background(), "my-proj", "99"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}

	// Project name mismatch
	mockStore = &mocks.MockRunStore{
		GetRunResult: &domain.Run{ID: 1, ProjectName: "other-proj", Status: "running"},
	}
	srv = newSSETestServer(t, mockStore)
	w = httptest.NewRecorder()
	srv.handleRunEvents(w, sseRequest(context.Background(), "my-proj", "1"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 on project mismatch", w.Code)
	}

	// Nil runStore
	srv.runStore = nil
	w = httptest.NewRecorder()
	srv.handleRunEvents(w, sseRequest(context.Background(), "my-proj", "1"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 on nil runStore", w.Code)
	}
}

type nonFlusherWriter struct {
	header http.Header
	code   int
	body   []byte
}

func (w *nonFlusherWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *nonFlusherWriter) Write(b []byte) (int, error) {
	w.body = append(w.body, b...)
	return len(b), nil
}
func (w *nonFlusherWriter) WriteHeader(code int) { w.code = code }

// erroringFlushWriter is an http.ResponseWriter + http.Flusher whose Write
// starts failing after failAfter successful calls, simulating a connection
// that died after headers already went out - see REVIEW.md R1: the handler
// must return instead of looping on a dead socket.
type erroringFlushWriter struct {
	header    http.Header
	code      int
	failAfter int
	writes    int
}

func (w *erroringFlushWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *erroringFlushWriter) Write(b []byte) (int, error) {
	w.writes++
	if w.writes > w.failAfter {
		return 0, errors.New("simulated broken pipe")
	}
	return len(b), nil
}
func (w *erroringFlushWriter) WriteHeader(code int) { w.code = code }
func (w *erroringFlushWriter) Flush()               {}

func TestHandlerSSE_HeartbeatWriteErrorReturns(t *testing.T) {
	mockStore := &mocks.MockRunStore{
		GetRunResult: &domain.Run{ID: 1, ProjectName: "my-proj", Status: "running"},
	}
	srv := newSSETestServer(t, mockStore)
	srv.sseHeartbeatInterval = time.Millisecond
	ew := &erroringFlushWriter{failAfter: 0}

	done := make(chan struct{})
	go func() {
		srv.handleRunEvents(ew, sseRequest(context.Background(), "my-proj", "1"))
		close(done)
	}()

	select {
	case <-done:
		// Returned once the heartbeat write failed.
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after a failed heartbeat write")
	}
}

func TestHandlerSSE_EventWriteErrorReturns(t *testing.T) {
	mockStore := &mocks.MockRunStore{
		GetRunResult: &domain.Run{ID: 1, ProjectName: "my-proj", Status: "running"},
	}
	srv := newSSETestServer(t, mockStore)
	srv.sseHeartbeatInterval = time.Hour
	ew := &erroringFlushWriter{failAfter: 0}

	done := make(chan struct{})
	go func() {
		srv.handleRunEvents(ew, sseRequest(context.Background(), "my-proj", "1"))
		close(done)
	}()

	for i := 0; i < 50; i++ {
		if srv.EventHub().SubscriberCount(1) > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	srv.EventHub().BroadcastTask(1, "build", "running", "")

	select {
	case <-done:
		// Returned once the event write failed.
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after a failed event write")
	}
}

func TestHandlerSSE_StreamingUnsupported(t *testing.T) {
	mockStore := &mocks.MockRunStore{
		GetRunResult: &domain.Run{ID: 1, ProjectName: "my-proj", Status: "running"},
	}
	srv := newSSETestServer(t, mockStore)
	nf := &nonFlusherWriter{}
	srv.handleRunEvents(nf, sseRequest(context.Background(), "my-proj", "1"))
	if nf.code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when Flusher unsupported", nf.code)
	}
}

func TestHandlerSSE_SlowConsumerChannelCloseExits(t *testing.T) {
	mockStore := &mocks.MockRunStore{
		GetRunResult: &domain.Run{ID: 1, ProjectName: "my-proj", Status: "running"},
	}
	// Buffer size 1 to force drop
	srv := newSSETestServer(t, mockStore)
	srv.eventHub = service.NewRunEventHubWithBuffer(1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := httptest.NewRecorder()
	req := sseRequest(ctx, "my-proj", "1")

	done := make(chan struct{})
	go func() {
		srv.handleRunEvents(w, req)
		close(done)
	}()

	// Wait until subscribed
	for i := 0; i < 50; i++ {
		if srv.EventHub().SubscriberCount(1) > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	// Overflow buffer so subscriber channel is dropped and closed
	srv.EventHub().BroadcastTrace(1, "build", "1", 0)
	srv.EventHub().BroadcastTrace(1, "build", "2", 1)
	srv.EventHub().BroadcastTrace(1, "build", "3", 2)

	select {
	case <-done:
		// Exited cleanly when channel closed
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handler did not exit when channel was closed")
	}
}

func TestServer_EventHubLazyInit(t *testing.T) {
	s := &Server{}
	hub := s.EventHub()
	if hub == nil {
		t.Fatal("EventHub() returned nil")
	}
	if s.EventHub() != hub {
		t.Fatal("EventHub() returned different instance")
	}
}

func TestHandlerSSE_AlreadyTerminalRunClosesImmediately(t *testing.T) {
	mockStore := &mocks.MockRunStore{
		GetRunResult: &domain.Run{ID: 1, ProjectName: "my-proj", Status: domain.RunStatusPassed},
	}
	srv := newSSETestServer(t, mockStore)

	w := httptest.NewRecorder()
	srv.handleRunEvents(w, sseRequest(context.Background(), "my-proj", "1"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	body := w.Body.String()
	if !strings.Contains(body, "event: complete") || !strings.Contains(body, `"status":"passed"`) {
		t.Fatalf("body does not contain complete event: %q", body)
	}
}

func TestHandlerSSE_StreamsEventsAndClosesOnComplete(t *testing.T) {
	mockStore := &mocks.MockRunStore{
		GetRunResult: &domain.Run{ID: 1, ProjectName: "my-proj", Status: "running"},
	}
	srv := newSSETestServer(t, mockStore)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := httptest.NewRecorder()
	req := sseRequest(ctx, "my-proj", "1")

	done := make(chan struct{})
	go func() {
		srv.handleRunEvents(w, req)
		close(done)
	}()

	// Wait briefly for subscriber registration
	for i := 0; i < 50; i++ {
		if srv.EventHub().SubscriberCount(1) > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if srv.EventHub().SubscriberCount(1) == 0 {
		t.Fatal("subscriber was not registered")
	}

	// Broadcast task transition
	srv.EventHub().BroadcastTask(1, "build", "running", "")
	// Broadcast trace chunk
	srv.EventHub().BroadcastTrace(1, "build", "step 1 finished\n", 0)
	// Broadcast complete
	srv.EventHub().BroadcastComplete(1, "passed")

	select {
	case <-done:
		// Stream cleanly terminated on complete
	case <-time.After(500 * time.Millisecond):
		t.Fatal("stream did not close on complete event")
	}

	body := w.Body.String()
	if !strings.Contains(body, "event: task\ndata: ") || !strings.Contains(body, `"task_name":"build"`) {
		t.Errorf("missing task event in %q", body)
	}
	if !strings.Contains(body, "event: trace\ndata: ") || !strings.Contains(body, `"chunk":"step 1 finished\n"`) {
		t.Errorf("missing trace event in %q", body)
	}
	if !strings.Contains(body, "event: complete\ndata: ") || !strings.Contains(body, `"status":"passed"`) {
		t.Errorf("missing complete event in %q", body)
	}

	// Subscriber should be cleaned up after exit
	if count := srv.EventHub().SubscriberCount(1); count != 0 {
		t.Errorf("SubscriberCount = %d, want 0 after stream close", count)
	}
}

func TestHandlerSSE_HeartbeatsEmitted(t *testing.T) {
	mockStore := &mocks.MockRunStore{
		GetRunResult: &domain.Run{ID: 1, ProjectName: "my-proj", Status: "running"},
	}
	srv := newSSETestServer(t, mockStore)
	srv.sseHeartbeatInterval = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := httptest.NewRecorder()
	req := sseRequest(ctx, "my-proj", "1")

	done := make(chan struct{})
	go func() {
		srv.handleRunEvents(w, req)
		close(done)
	}()

	// Wait long enough for heartbeats
	time.Sleep(65 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("handler did not exit on context cancellation")
	}

	body := w.Body.String()
	count := strings.Count(body, ": ping\n\n")
	if count < 2 {
		t.Fatalf("heartbeat count = %d in %q, want at least 2", count, body)
	}
}

func TestHandlerSSE_ClientDisconnectExitsCleanly(t *testing.T) {
	mockStore := &mocks.MockRunStore{
		GetRunResult: &domain.Run{ID: 1, ProjectName: "my-proj", Status: "running"},
	}
	srv := newSSETestServer(t, mockStore)

	ctx, cancel := context.WithCancel(context.Background())
	w := httptest.NewRecorder()
	req := sseRequest(ctx, "my-proj", "1")

	done := make(chan struct{})
	go func() {
		srv.handleRunEvents(w, req)
		close(done)
	}()

	// Wait until subscribed
	for i := 0; i < 50; i++ {
		if srv.EventHub().SubscriberCount(1) > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	// Cancel context (client disconnect)
	cancel()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("handler did not exit on client disconnect")
	}

	if count := srv.EventHub().SubscriberCount(1); count != 0 {
		t.Errorf("SubscriberCount = %d, want 0 after disconnect", count)
	}
}

// TestSSE_HandlerStreamsHubEventsOverRealHTTP drives handleRunEvents behind
// a real net/http server and broadcasts events directly on the hub, to
// verify the HTTP/SSE framing and hub-to-client plumbing end to end. It
// does not drive a real RunService execution - MockPipelineRunner.Run
// never calls opts.OnTaskStart/OnTaskComplete, so there is no task/trace
// callback chain to exercise through it - so this does not prove a real
// pipeline run reaches the client, only that whatever the hub broadcasts
// does (see REVIEW.md R14; this test was previously named
// TestSSE_EndToEnd_StreamingRunExecution, which overstated that).
func TestSSE_HandlerStreamsHubEventsOverRealHTTP(t *testing.T) {
	mockStore := &mocks.MockRunStore{
		GetRunResult: &domain.Run{ID: 42, ProjectName: "test-proj", Status: "running"},
	}
	srv := setupRunServer(t, &mocks.MockProjectStore{}, mockStore)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chiCtx := chi.NewRouteContext()
		chiCtx.URLParams.Add("name", "test-proj")
		chiCtx.URLParams.Add("runID", "42")
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtx))
		srv.handleRunEvents(w, r)
	}))
	defer ts.Close()

	// Start HTTP client request
	res, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET /events failed: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	// Wait briefly for subscriber registration
	for i := 0; i < 50; i++ {
		if srv.EventHub().SubscriberCount(42) > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	// Trigger pipeline lifecycle callbacks
	hub := srv.EventHub()
	hub.BroadcastTask(42, "unit-test", "running", "")
	hub.BroadcastTrace(42, "unit-test", "running test 1 of 5\n", 0)
	hub.BroadcastTask(42, "unit-test", "passed", "")
	hub.BroadcastStatus(42, "passed", "")
	hub.BroadcastComplete(42, "passed")

	// Read stream until EOF
	scanner := bufio.NewScanner(res.Body)
	var eventsReceived []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventsReceived = append(eventsReceived, strings.TrimPrefix(line, "event: "))
		}
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("stream read error: %v", err)
	}

	expected := []string{"task", "trace", "task", "status", "complete"}
	if len(eventsReceived) != len(expected) {
		t.Fatalf("events received = %v, want %v", eventsReceived, expected)
	}
	for i, exp := range expected {
		if eventsReceived[i] != exp {
			t.Errorf("event[%d] = %q, want %q", i, eventsReceived[i], exp)
		}
	}
}
