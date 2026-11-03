package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"kestrix/internal/domain"
)

// ── Task 2.1: Chunks queued on HTTP network errors instead of discarded ──

func TestTraceWriter_NetworkErrorQueuesIntoSpool(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	badURL := ts.URL
	ts.Close() // immediately close to force network errors

	tw := newTraceWriter(context.Background(), http.DefaultClient, badURL, "tok", 1, 1, 1024*1024)
	defer tw.Stop()

	chunk1 := []byte("first line of output\n")
	chunk2 := []byte("second line of output\n")

	n1, err1 := tw.Write(chunk1)
	if err1 != nil {
		t.Fatalf("Write must return nil error, got %v", err1)
	}
	if n1 != len(chunk1) {
		t.Fatalf("n1 = %d, want %d", n1, len(chunk1))
	}

	if tw.spool.Len() != 1 {
		t.Fatalf("expected 1 chunk in spool after network error, got %d", tw.spool.Len())
	}
	if tw.spool.BufferedBytes() != len(chunk1) {
		t.Fatalf("buffered bytes = %d, want %d", tw.spool.BufferedBytes(), len(chunk1))
	}

	n2, err2 := tw.Write(chunk2)
	if err2 != nil {
		t.Fatalf("Write must return nil error, got %v", err2)
	}
	if n2 != len(chunk2) {
		t.Fatalf("n2 = %d, want %d", n2, len(chunk2))
	}

	if tw.spool.Len() != 2 {
		t.Fatalf("expected 2 chunks in spool, got %d", tw.spool.Len())
	}
	if tw.spool.BufferedBytes() != len(chunk1)+len(chunk2) {
		t.Fatalf("buffered bytes = %d, want %d", tw.spool.BufferedBytes(), len(chunk1)+len(chunk2))
	}

	chunks := tw.spool.Chunks()
	if chunks[0].Offset != 0 || string(chunks[0].Data) != string(chunk1) {
		t.Fatalf("chunk 0 mismatch: offset=%d data=%q", chunks[0].Offset, string(chunks[0].Data))
	}
	if chunks[1].Offset != len(chunk1) || string(chunks[1].Data) != string(chunk2) {
		t.Fatalf("chunk 1 mismatch: offset=%d data=%q", chunks[1].Offset, string(chunks[1].Data))
	}
}

// ── Task 2.2: Background retry flusher with exponential backoff and jitter ──

func TestTraceWriter_RetryFlusherContiguousSequentialOffsets(t *testing.T) {
	origBase := traceRetryBaseDelay
	origMax := traceRetryMaxDelay
	traceRetryBaseDelay = 5 * time.Millisecond
	traceRetryMaxDelay = 20 * time.Millisecond
	t.Cleanup(func() {
		traceRetryBaseDelay = origBase
		traceRetryMaxDelay = origMax
	})

	var mu sync.Mutex
	type receivedTrace struct {
		offset int
		chunk  string
	}
	var received []receivedTrace
	var attempts atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		att := attempts.Add(1)
		// Fail the first 2 attempts with HTTP 500
		if att <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		var body struct {
			Epoch  int    `json:"epoch"`
			Offset int    `json:"offset"`
			Chunk  string `json:"chunk"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		mu.Lock()
		received = append(received, receivedTrace{offset: body.Offset, chunk: body.Chunk})
		mu.Unlock()

		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	tw := newTraceWriter(ctx, ts.Client(), ts.URL, "tok", 1, 1, 1024*1024)
	defer tw.Stop()

	// Write three chunks
	tw.Write([]byte("chunk-alpha\n"))
	tw.Write([]byte("chunk-beta\n"))
	tw.Write([]byte("chunk-gamma\n"))

	// Flush waits for all chunks to be delivered
	if err := tw.Flush(ctx); err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 3 {
		t.Fatalf("expected 3 received chunks, got %d", len(received))
	}

	// Verify contiguous sequential offsets
	expectedOffset := 0
	expectedChunks := []string{"chunk-alpha\n", "chunk-beta\n", "chunk-gamma\n"}
	for i, rc := range received {
		if rc.offset != expectedOffset {
			t.Fatalf("chunk %d: offset = %d, want %d", i, rc.offset, expectedOffset)
		}
		if rc.chunk != expectedChunks[i] {
			t.Fatalf("chunk %d: chunk = %q, want %q", i, rc.chunk, expectedChunks[i])
		}
		expectedOffset += len(rc.chunk)
	}
}

func TestTraceWriter_MidStreamNetworkOutagePreservesContiguousOffsets(t *testing.T) {
	origBase := traceRetryBaseDelay
	origMax := traceRetryMaxDelay
	traceRetryBaseDelay = 5 * time.Millisecond
	traceRetryMaxDelay = 20 * time.Millisecond
	t.Cleanup(func() {
		traceRetryBaseDelay = origBase
		traceRetryMaxDelay = origMax
	})

	var mu sync.Mutex
	type receivedTrace struct {
		offset int
		chunk  string
	}
	var received []receivedTrace
	var serverFailing atomic.Bool

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serverFailing.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		var body struct {
			Epoch  int    `json:"epoch"`
			Offset int    `json:"offset"`
			Chunk  string `json:"chunk"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		mu.Lock()
		received = append(received, receivedTrace{offset: body.Offset, chunk: body.Chunk})
		mu.Unlock()

		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tw := newTraceWriter(ctx, ts.Client(), ts.URL, "tok", 1, 1, 1024*1024)
	defer tw.Stop()

	// 1. Initial write while network is healthy
	chunk0 := []byte("line-0: healthy transmission\n")
	tw.Write(chunk0)
	if err := tw.Flush(ctx); err != nil {
		t.Fatalf("Flush chunk0 failed: %v", err)
	}

	mu.Lock()
	if len(received) != 1 || received[0].chunk != string(chunk0) {
		mu.Unlock()
		t.Fatalf("expected initial chunk received, got %+v", received)
	}
	mu.Unlock()

	// 2. Trigger mid-stream network outage
	serverFailing.Store(true)

	// Write subsequent chunks during the outage
	chunk1 := []byte("line-1: sent during network outage\n")
	chunk2 := []byte("line-2: also spooled during outage\n")
	tw.Write(chunk1)
	tw.Write(chunk2)

	// Ensure chunks are held in spool and not discarded
	time.Sleep(30 * time.Millisecond)
	if tw.spool.Len() < 2 {
		t.Fatalf("expected at least 2 chunks spooled during outage, got %d", tw.spool.Len())
	}

	// 3. Restore network connectivity and write a final chunk
	serverFailing.Store(false)
	chunk3 := []byte("line-3: sent after network recovery\n")
	tw.Write(chunk3)

	// Wait for all spooled and new chunks to flush
	if err := tw.Flush(ctx); err != nil {
		t.Fatalf("Flush after network recovery failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	expectedChunks := []string{
		string(chunk0),
		string(chunk1),
		string(chunk2),
		string(chunk3),
	}

	if len(received) != len(expectedChunks) {
		t.Fatalf("expected %d received chunks, got %d: %+v", len(expectedChunks), len(received), received)
	}

	expectedOffset := 0
	for i, rc := range received {
		if rc.offset != expectedOffset {
			t.Fatalf("chunk %d: offset = %d, want %d", i, rc.offset, expectedOffset)
		}
		if rc.chunk != expectedChunks[i] {
			t.Fatalf("chunk %d: chunk = %q, want %q", i, rc.chunk, expectedChunks[i])
		}
		expectedOffset += len(rc.chunk)
	}
}

// ── Task 2.3: HTTP 409 Conflict clears spool and stops retries on stale epoch ──

func TestTraceWriter_HTTP409Conflict_StopsRetriesAndClearsSpool(t *testing.T) {
	origBase := traceRetryBaseDelay
	traceRetryBaseDelay = 5 * time.Millisecond
	t.Cleanup(func() {
		traceRetryBaseDelay = origBase
	})

	var requestCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusConflict) // 409 Conflict
	}))
	defer ts.Close()

	tw := newTraceWriter(context.Background(), ts.Client(), ts.URL, "tok", 1, 1, 1024*1024)
	defer tw.Stop()

	tw.Write([]byte("some trace log\n"))

	// Give flusher a moment to process the 409 Conflict
	time.Sleep(50 * time.Millisecond)

	if !tw.spool.IsEmpty() {
		t.Fatalf("expected spool to be empty after 409 Conflict, got len %d", tw.spool.Len())
	}
	if !tw.spool.IsDead() {
		t.Fatal("expected spool to be marked dead after 409 Conflict")
	}

	initialCount := requestCount.Load()
	if initialCount == 0 {
		t.Fatal("expected at least 1 request to have been made")
	}

	// Wait further and verify NO further retry requests are sent
	time.Sleep(50 * time.Millisecond)
	if requestCount.Load() != initialCount {
		t.Fatalf("expected request count to stay at %d (no retries after 409), but got %d", initialCount, requestCount.Load())
	}
}

// ── Task 2.4: Pre-completion flush in reportComplete waits for spool to empty ──

func TestReportComplete_PreCompletionFlushWaitsForSpool(t *testing.T) {
	var completeReceived atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/api/runner-jobs/1/complete" {
			completeReceived.Store(true)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	spool := NewTraceSpool(1024, 0)
	spool.Append([]byte("unsent trace chunk"))

	completeDone := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go func() {
		err := reportComplete(ctx, ts.Client(), ts.URL, "tok", 1, 1, "passed", "", "", spool)
		completeDone <- err
	}()

	// Verify completion is NOT sent while spool has chunks
	time.Sleep(30 * time.Millisecond)
	if completeReceived.Load() {
		t.Fatal("reportComplete sent completion request before spool was drained!")
	}

	// Drain the spool
	spool.Pop()

	select {
	case err := <-completeDone:
		if err != nil {
			t.Fatalf("reportComplete returned error: %v", err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("reportComplete did not finish after spool was drained")
	}

	if !completeReceived.Load() {
		t.Fatal("expected complete request to be sent after spool drained")
	}
}

func TestReportComplete_PreCompletionFlushTimesOut(t *testing.T) {
	spool := NewTraceSpool(1024, 0)
	spool.Append([]byte("chunk never acknowledged"))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := reportComplete(ctx, http.DefaultClient, "http://127.0.0.1:9999", "tok", 1, 1, "passed", "", "", spool)
	if err == nil {
		t.Fatal("expected reportComplete to fail when spool flush times out")
	}
}

// ── Task 3.2: End-to-end simulation test with transient failures ──

func TestRunner_EndToEndSimulation_TransientFailuresPreserveTrace(t *testing.T) {
	origBase := traceRetryBaseDelay
	origMax := traceRetryMaxDelay
	traceRetryBaseDelay = 5 * time.Millisecond
	traceRetryMaxDelay = 20 * time.Millisecond
	t.Cleanup(func() {
		traceRetryBaseDelay = origBase
		traceRetryMaxDelay = origMax
	})

	var mu sync.Mutex
	var receivedTraceChunks []string
	var serverTraceOffset int
	var completedStatus string
	var traceAttempts atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPatch && r.URL.Path == "/api/runner-jobs/1/trace":
			att := traceAttempts.Add(1)
			// Simulate transient 500 errors on every 2nd attempt
			if att%2 == 1 && att <= 5 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			var body struct {
				Epoch  int    `json:"epoch"`
				Offset int    `json:"offset"`
				Chunk  string `json:"chunk"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			mu.Lock()
			// Assert no offset gap
			if body.Offset != serverTraceOffset {
				mu.Unlock()
				http.Error(w, fmt.Sprintf("offset mismatch: got %d, expected %d", body.Offset, serverTraceOffset), http.StatusBadRequest)
				return
			}
			serverTraceOffset += len(body.Chunk)
			receivedTraceChunks = append(receivedTraceChunks, body.Chunk)
			mu.Unlock()

			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodPut && r.URL.Path == "/api/runner-jobs/1/complete":
			var body struct {
				Status string `json:"status"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			completedStatus = body.Status
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create job payload that executes a shell script writing 4 distinct lines
	job := &domain.RemoteJob{
		ID:         1,
		ClaimEpoch: 1,
		TaskName:   "build",
		Payload: domain.RemoteJobPayload{
			Task: domain.Task{
				Name: "build",
				Execute: domain.ExecuteSpec{
					Script: "echo line-one; echo line-two; echo line-three; echo line-four",
				},
			},
		},
	}

	runJob(ctx, ts.Client(), ts.URL, "tok", "local", job, 1024*1024)

	mu.Lock()
	defer mu.Unlock()

	if completedStatus != "passed" {
		t.Fatalf("job completed with status %q, want passed", completedStatus)
	}

	fullTrace := ""
	for _, c := range receivedTraceChunks {
		fullTrace += c
	}

	for _, expectedLine := range []string{"line-one", "line-two", "line-three", "line-four"} {
		if !strings.Contains(fullTrace, expectedLine) {
			t.Fatalf("expected trace output to contain %q, but got:\n%s", expectedLine, fullTrace)
		}
	}
}
